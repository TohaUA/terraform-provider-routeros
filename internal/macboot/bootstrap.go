// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package macboot

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// NonceEnvelopeBeginPrefix marks the start of structured bootstrap output.
	NonceEnvelopeBeginPrefix = "===MACBOOT-BEGIN-"
	// NonceEnvelopeEndPrefix marks the end of structured bootstrap output.
	NonceEnvelopeEndPrefix = "===MACBOOT-END-"
	// DefaultCertificateName is the default RouterOS certificate name created by bootstrap.
	DefaultCertificateName = "macboot-tls"
)

// ErrInsecureL2Required is returned when a caller attempts MAC-Telnet bootstrap
// without explicitly opting in via AllowInsecureL2.
var ErrInsecureL2Required = errors.New("refusing layer-2 MAC-Telnet bootstrap: allow_insecure_l2 must be explicitly set to true")

// BootstrapConfig configures a layer-2 MAC-Telnet bootstrap run.
type BootstrapConfig struct {
	Interface              string
	MACAddress             string
	Address                string
	TargetInterface        string
	Username               string
	Password               string
	AllowInsecureL2        bool
	ExpectedTLSFingerprint string
	CertificateName        string
	EnableAPISSL           bool
	Timeout                time.Duration
	StrictPin              bool
	UnsafeRawDebug         bool
	Logger                 func(format string, args ...any)

	// Test hooks (unexported so external callers use real MAC-Telnet + HTTPS REST).
	sessionRunner func(ctx context.Context, cfg BootstrapConfig, script string) (string, error)
	restAddr      string // host:port override for TLS REST verification tests
}

// BootstrapResult contains the verified outputs of a layer-2 bootstrap.
type BootstrapResult struct {
	TLSFingerprint  string `json:"tls_fingerprint"`
	Address         string `json:"address"`
	RouterAddress   string `json:"router_address"`
	Identity        string `json:"identity"`
	RouterOSVersion string `json:"routeros_version"`
	Warning         string `json:"warning,omitempty"`
}

// QuoteRouterOSString safely quotes a string for RouterOS CLI double-quoted
// literals, rejecting control characters and escaping \, ", $, and ?.
func QuoteRouterOSString(s string) (string, error) {
	if !utf8.ValidString(s) {
		return "", errors.New("string is not valid UTF-8")
	}
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("control character 0x%02x is not allowed in RouterOS string", r)
		}
		switch r {
		case '\\', '"', '$', '?':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String(), nil
}

// NormalizeFingerprint normalizes a SHA-256 fingerprint (optional sha256: prefix,
// colons, or spaces) to 64 uppercase hex characters without colons.
func NormalizeFingerprint(raw string) (string, error) {
	clean := strings.TrimSpace(raw)
	if len(clean) >= len("sha256:") && strings.EqualFold(clean[:len("sha256:")], "sha256:") {
		clean = clean[len("sha256:"):]
	}
	clean = strings.ReplaceAll(clean, ":", "")
	clean = strings.ReplaceAll(clean, " ", "")
	clean = strings.ToUpper(clean)
	if len(clean) != 64 {
		return "", fmt.Errorf("expected 64 hex characters (SHA-256), got length %d", len(clean))
	}
	if _, err := hex.DecodeString(clean); err != nil {
		return "", fmt.Errorf("invalid SHA-256 hex fingerprint: %w", err)
	}
	return clean, nil
}

// ParseBootstrapOutput parses key=value bootstrap metadata strictly bounded by
// matching nonce begin/end markers.
func ParseBootstrapOutput(raw string, nonce string) (BootstrapResult, error) {
	if nonce == "" || strings.ContainsAny(nonce, " \t\r\n") {
		return BootstrapResult{}, errors.New("invalid bootstrap nonce")
	}

	beginMarker := NonceEnvelopeBeginPrefix + nonce + "==="
	endMarker := NonceEnvelopeEndPrefix + nonce + "==="

	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	lines := strings.Split(normalized, "\n")

	inBlock := false
	foundBegin := false
	foundEnd := false
	kv := make(map[string]string)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == beginMarker {
			if foundBegin {
				return BootstrapResult{}, errors.New("duplicate bootstrap begin marker")
			}
			foundBegin = true
			inBlock = true
			continue
		}
		if trimmed == endMarker {
			if !inBlock {
				return BootstrapResult{}, errors.New("bootstrap end marker appeared without matching begin marker")
			}
			foundEnd = true
			inBlock = false
			break
		}
		if !inBlock || trimmed == "" {
			continue
		}
		key, val, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key != "" {
			kv[key] = val
		}
	}

	if !foundBegin {
		return BootstrapResult{}, errors.New("missing bootstrap begin marker")
	}
	if !foundEnd {
		return BootstrapResult{}, errors.New("missing bootstrap end marker")
	}

	fpRaw := kv["fingerprint"]
	if fpRaw == "" {
		return BootstrapResult{}, errors.New("bootstrap output missing fingerprint")
	}
	fp, err := NormalizeFingerprint(fpRaw)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("bootstrap output invalid fingerprint: %w", err)
	}

	addr := kv["address"]
	if addr == "" {
		return BootstrapResult{}, errors.New("bootstrap output missing address")
	}
	identity := kv["identity"]
	if identity == "" {
		return BootstrapResult{}, errors.New("bootstrap output missing identity")
	}
	version := kv["version"]
	if version == "" {
		return BootstrapResult{}, errors.New("bootstrap output missing version")
	}

	ip, _, err := net.ParseCIDR(addr)
	if err != nil || ip.To4() == nil {
		// Also accept bare IPv4 if RouterOS ever omits the prefix, though /ip/address/get returns CIDR.
		parsedIP := net.ParseIP(addr)
		if parsedIP == nil || parsedIP.To4() == nil {
			return BootstrapResult{}, fmt.Errorf("bootstrap output invalid IPv4 address %q", addr)
		}
		ip = parsedIP
	}

	return BootstrapResult{
		TLSFingerprint:  fp,
		Address:         addr,
		RouterAddress:   ip.To4().String(),
		Identity:        identity,
		RouterOSVersion: version,
	}, nil
}

// BuildBootstrapScript constructs an idempotent RouterOS CLI script to assign
// an IPv4 address, create/sign a TLS server certificate if absent, enable
// www-ssl (and optionally api-ssl), and print structured metadata wrapped in
// nonce markers. The script never includes any password or secret.
func BuildBootstrapScript(cfg BootstrapConfig, nonce string) (string, error) {
	if strings.TrimSpace(cfg.TargetInterface) == "" {
		return "", errors.New("target_interface must not be empty")
	}
	ip, _, err := net.ParseCIDR(strings.TrimSpace(cfg.Address))
	if err != nil || ip.To4() == nil {
		return "", fmt.Errorf("address %q must be a valid IPv4 CIDR (for example 192.168.88.1/24)", cfg.Address)
	}
	certName := strings.TrimSpace(cfg.CertificateName)
	if certName == "" {
		certName = DefaultCertificateName
	}
	if nonce == "" || strings.ContainsAny(nonce, " \t\r\n") {
		return "", errors.New("nonce must be non-empty and contain no whitespace")
	}

	qIface, err := QuoteRouterOSString(strings.TrimSpace(cfg.TargetInterface))
	if err != nil {
		return "", fmt.Errorf("target_interface: %w", err)
	}
	qAddr, err := QuoteRouterOSString(strings.TrimSpace(cfg.Address))
	if err != nil {
		return "", fmt.Errorf("address: %w", err)
	}
	qCert, err := QuoteRouterOSString(certName)
	if err != nil {
		return "", fmt.Errorf("certificate_name: %w", err)
	}
	qNonce, err := QuoteRouterOSString(nonce)
	if err != nil {
		return "", fmt.Errorf("nonce: %w", err)
	}

	// Lines are kept well under 160 columns and split marker literals so command
	// echoes cannot match ParseBootstrapOutput markers.
	lines := []string{
		":global mbI " + qIface,
		":global mbA " + qAddr,
		":global mbC " + qCert,
		":global mbN " + qNonce,
		`:if ([:len [/ip/address/find address=$mbA interface=$mbI]]=0) do={/ip/address/add address=$mbA interface=$mbI}`,
		`:if ([:len [/certificate/find name=$mbC]]=0) do={/certificate/add name=$mbC common-name=routeros key-size=prime256v1 key-usage=tls-server}`,
		`:if ([:len [/certificate/find name=$mbC fingerprint=""]]>0) do={/certificate/sign ([/certificate/find name=$mbC]->0)}`,
		`/ip/service/set [find name="www-ssl"] certificate=$mbC disabled=no`,
	}
	if cfg.EnableAPISSL {
		lines = append(lines, `/ip/service/set [find name="api-ssl"] certificate=$mbC disabled=no`)
	}
	lines = append(lines,
		`:put ("===MACBOOT-" . "BEGIN-" . $mbN . "===")`,
		`:put ("fingerprint=" . [/certificate/get ([/certificate/find name=$mbC]->0) fingerprint])`,
		`:put ("address=" . [/ip/address/get ([/ip/address/find address=$mbA interface=$mbI]->0) address])`,
		`:put ("identity=" . [/system/identity/get name])`,
		`:put ("version=" . [/system/resource/get version])`,
		`:put ("===MACBOOT-" . "END-" . $mbN . "===")`,
		`/system/script/environment/remove [find name~"^mb[ACIN]$"]`,
	)

	return strings.Join(lines, "\n"), nil
}

// Bootstrap executes a layer-2 MAC-Telnet bootstrap against a RouterOS device
// and verifies the resulting HTTPS REST endpoint over pinned TLS.
func Bootstrap(ctx context.Context, cfg BootstrapConfig) (BootstrapResult, error) {
	if !cfg.AllowInsecureL2 {
		return BootstrapResult{}, ErrInsecureL2Required
	}
	if strings.TrimSpace(cfg.Interface) == "" {
		return BootstrapResult{}, errors.New("interface must not be empty")
	}
	hw, err := net.ParseMAC(strings.TrimSpace(cfg.MACAddress))
	if err != nil || len(hw) != 6 {
		return BootstrapResult{}, fmt.Errorf("invalid mac_address %q", cfg.MACAddress)
	}
	cfg.MACAddress = formatMACUpper(hw)

	if strings.TrimSpace(cfg.Username) == "" {
		return BootstrapResult{}, errors.New("username must not be empty")
	}
	if cfg.CertificateName == "" {
		cfg.CertificateName = DefaultCertificateName
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}

	var expectedFP string
	if strings.TrimSpace(cfg.ExpectedTLSFingerprint) != "" {
		expectedFP, err = NormalizeFingerprint(cfg.ExpectedTLSFingerprint)
		if err != nil {
			return BootstrapResult{}, fmt.Errorf("invalid expected_tls_fingerprint: %w", err)
		}
	}

	nonce, err := randomHexNonce(16)
	if err != nil {
		return BootstrapResult{}, err
	}
	script, err := BuildBootstrapScript(cfg, nonce)
	if err != nil {
		return BootstrapResult{}, err
	}

	bootCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	var rawOut string
	if cfg.sessionRunner != nil {
		rawOut, err = cfg.sessionRunner(bootCtx, cfg, script)
	} else {
		rawOut, err = runMACTelnetScript(bootCtx, cfg, script)
	}
	if err != nil {
		return BootstrapResult{}, err
	}

	res, err := ParseBootstrapOutput(rawOut, nonce)
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("parse bootstrap output: %w", err)
	}

	if expectedFP != "" {
		if subtle.ConstantTimeCompare([]byte(res.TLSFingerprint), []byte(expectedFP)) != 1 {
			return BootstrapResult{}, fmt.Errorf(
				"bootstrap TLS fingerprint mismatch: router presented %s, expected %s",
				res.TLSFingerprint, expectedFP,
			)
		}
	}

	targetAddr := cfg.restAddr
	if targetAddr == "" {
		targetAddr = net.JoinHostPort(res.RouterAddress, strconv.Itoa(443))
	}

	if err := verifyBootstrappedREST(bootCtx, targetAddr, cfg.Username, cfg.Password, res.TLSFingerprint); err != nil {
		return BootstrapResult{}, fmt.Errorf("verify bootstrapped HTTPS REST endpoint: %w", err)
	}

	if expectedFP == "" {
		res.Warning = "expected_tls_fingerprint was not provided; bootstrap accepted the router TLS certificate via unauthenticated layer-2 TOFU. Rotate the admin password over TLS and pin tls_fingerprint in subsequent Terraform configurations."
	}

	return res, nil
}

func runMACTelnetScript(ctx context.Context, cfg BootstrapConfig, script string) (string, error) {
	sess, err := DialMACTelnet(ctx, MACTelnetOptions{
		Interface:      cfg.Interface,
		TargetMAC:      cfg.MACAddress,
		Username:       cfg.Username,
		Password:       cfg.Password,
		Timeout:        cfg.Timeout,
		StrictPin:      cfg.StrictPin,
		UnsafeRawDebug: cfg.UnsafeRawDebug,
		Logger:         cfg.Logger,
	})
	if err != nil {
		return "", err
	}
	defer sess.Close()

	return sess.Exec(ctx, script)
}

func randomHexNonce(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func verifyBootstrappedREST(ctx context.Context, hostPort, username, password, expectedFP string) error {
	tlsCfg := &tls.Config{
		InsecureSkipVerify: true, // Leaf SHA-256 is verified directly below.
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			return checkCertFingerprint(rawCerts, expectedFP)
		},
	}

	// Custom VerifyPeerCertificate runs before TLS handshake completes.
	transport := &http.Transport{
		TLSClientConfig: tlsCfg,
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}

	url := "https://" + hostPort + "/rest/system/resource"
	var lastErr error
	for attempt := 0; attempt < 8; attempt++ {
		if ctx.Err() != nil {
			if lastErr != nil {
				return lastErr
			}
			return ctx.Err()
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.SetBasicAuth(username, password)
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			// Fail immediately on TLS fingerprint mismatch rather than retrying.
			if strings.Contains(err.Error(), "fingerprint") {
				return err
			}
			select {
			case <-ctx.Done():
				return lastErr
			case <-time.After(300 * time.Millisecond):
			}
			continue
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()

		if resp.TLS == nil || len(resp.TLS.PeerCertificates) == 0 {
			return errors.New("REST server did not present a TLS peer certificate")
		}
		if err := checkCertFingerprint([][]byte{resp.TLS.PeerCertificates[0].Raw}, expectedFP); err != nil {
			return err
		}
		if readErr != nil {
			return fmt.Errorf("read /rest/system/resource response: %w", readErr)
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("/rest/system/resource returned HTTP %d", resp.StatusCode)
		}
		var parsed map[string]any
		if err := json.Unmarshal(body, &parsed); err != nil {
			return fmt.Errorf("invalid JSON from /rest/system/resource: %w", err)
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return errors.New("REST verification failed")
}

func checkCertFingerprint(rawCerts [][]byte, expectedUpperHex string) error {
	if len(rawCerts) == 0 {
		return errors.New("TLS fingerprint verification failed: server presented no certificate")
	}
	sum := sha256.Sum256(rawCerts[0])
	got := strings.ToUpper(hex.EncodeToString(sum[:]))
	if subtle.ConstantTimeCompare([]byte(got), []byte(expectedUpperHex)) != 1 {
		return fmt.Errorf("TLS fingerprint verification failed: server certificate SHA-256 %s does not match expected %s", got, expectedUpperHex)
	}
	return nil
}
