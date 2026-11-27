// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package macboot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestQuoteRouterOSString(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "ether2", want: `"ether2"`},
		{in: "192.168.88.1/24", want: `"192.168.88.1/24"`},
		{in: `a\b"c$d?e`, want: `"a\\b\"c\$d\?e"`},
		{in: "bad\nline", wantErr: true},
		{in: "bad\rline", wantErr: true},
		{in: "bad\x00null", wantErr: true},
		{in: "bad\x7fdel", wantErr: true},
		{in: string([]byte{0xff, 0xfe}), wantErr: true},
	}
	for _, tc := range tests {
		got, err := QuoteRouterOSString(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("QuoteRouterOSString(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("QuoteRouterOSString(%q) unexpected error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("QuoteRouterOSString(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildBootstrapScriptContainsNoSecrets(t *testing.T) {
	secret := "SuperSecretPassword!12345"
	cfg := BootstrapConfig{
		TargetInterface: "ether2",
		Address:         "172.30.99.2/24",
		Username:        "admin",
		Password:        secret,
		CertificateName: "macboot-tls",
		EnableAPISSL:    true,
	}
	script, err := BuildBootstrapScript(cfg, "deadbeef1234")
	if err != nil {
		t.Fatalf("BuildBootstrapScript failed: %v", err)
	}
	if strings.Contains(script, secret) {
		t.Fatalf("bootstrap script leaked password")
	}
	if !strings.Contains(script, "www-ssl") || !strings.Contains(script, "api-ssl") {
		t.Fatalf("expected www-ssl and api-ssl configuration in script:\n%s", script)
	}
	for _, line := range strings.Split(script, "\n") {
		if len(line) >= int(macTelnetTermWidth) {
			t.Fatalf("script line exceeds terminal width %d (%d): %s", macTelnetTermWidth, len(line), line)
		}
	}
}

func TestParseBootstrapOutput(t *testing.T) {
	fp := strings.Repeat("ab", 32)
	raw := "echo :put (\"===MACBOOT-\" . \"BEGIN-n123===\")\r\n" +
		"===MACBOOT-BEGIN-n123===\r\n" +
		"fingerprint=" + strings.ToLower(fp) + "\r\n" +
		"address=172.30.99.2/24\r\n" +
		"identity=MikroTik\r\n" +
		"version=7.25.2 (stable)\r\n" +
		"===MACBOOT-END-n123===\r\n" +
		"[admin@MikroTik] > "

	res, err := ParseBootstrapOutput(raw, "n123")
	if err != nil {
		t.Fatalf("ParseBootstrapOutput failed: %v", err)
	}
	if res.TLSFingerprint != strings.ToUpper(fp) {
		t.Fatalf("TLSFingerprint = %q, want %q", res.TLSFingerprint, strings.ToUpper(fp))
	}
	if res.RouterAddress != "172.30.99.2" || res.Identity != "MikroTik" || res.RouterOSVersion != "7.25.2 (stable)" {
		t.Fatalf("unexpected result: %+v", res)
	}

	badInputs := []string{
		"",
		"===MACBOOT-BEGIN-n123===\nfingerprint=" + fp,
		"===MACBOOT-END-n123===",
		"===MACBOOT-BEGIN-other===\nfingerprint=" + fp + "\naddress=1.2.3.4/24\nidentity=r\nversion=7.25\n===MACBOOT-END-other===",
		"===MACBOOT-BEGIN-n123===\nfingerprint=bad\naddress=1.2.3.4/24\nidentity=r\nversion=7.25\n===MACBOOT-END-n123===",
		"===MACBOOT-BEGIN-n123===\nfingerprint=" + fp + "\naddress=1.2.3.4/24\nidentity=r\n===MACBOOT-END-n123===",
	}
	for i, bad := range badInputs {
		if _, err := ParseBootstrapOutput(bad, "n123"); err == nil {
			t.Fatalf("case %d: expected ParseBootstrapOutput error", i)
		}
	}
}

func FuzzParseBootstrapOutput(f *testing.F) {
	f.Add("===MACBOOT-BEGIN-abc===\nfingerprint="+strings.Repeat("00", 32)+"\naddress=10.0.0.1/24\nidentity=r\nversion=7.25\n===MACBOOT-END-abc===", "abc")
	f.Fuzz(func(t *testing.T, raw, nonce string) {
		_, _ = ParseBootstrapOutput(raw, nonce)
	})
}

func TestBootstrapRefusesWithoutOptIn(t *testing.T) {
	_, err := Bootstrap(context.Background(), BootstrapConfig{
		Interface:       "eth0",
		MACAddress:      "54:05:AB:01:02:03",
		Address:         "172.30.99.2/24",
		TargetInterface: "ether2",
		Username:        "admin",
		AllowInsecureL2: false,
	})
	if !errors.Is(err, ErrInsecureL2Required) {
		t.Fatalf("expected ErrInsecureL2Required, got %v", err)
	}
}

func TestBootstrapFingerprintMismatchFailsClosed(t *testing.T) {
	fp := strings.Repeat("AA", 32)
	wrongFP := strings.Repeat("BB", 32)

	restCalled := false
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		restCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, err := Bootstrap(context.Background(), BootstrapConfig{
		Interface:              "eth0",
		MACAddress:             "54:05:AB:01:02:03",
		Address:                "172.30.99.2/24",
		TargetInterface:        "ether2",
		Username:               "admin",
		Password:               "",
		AllowInsecureL2:        true,
		ExpectedTLSFingerprint: wrongFP,
		restAddr:               strings.TrimPrefix(srv.URL, "https://"),
		sessionRunner: func(ctx context.Context, cfg BootstrapConfig, script string) (string, error) {
			nonce := extractNonceFromScript(t, script)
			return fmt.Sprintf("===MACBOOT-BEGIN-%s===\nfingerprint=%s\naddress=172.30.99.2/24\nidentity=MikroTik\nversion=7.25.2\n===MACBOOT-END-%s===\n", nonce, fp, nonce), nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "fingerprint mismatch") {
		t.Fatalf("expected fingerprint mismatch error, got %v", err)
	}
	if restCalled {
		t.Fatalf("REST verification must not run after fingerprint mismatch")
	}
}

func extractNonceFromScript(t *testing.T, script string) string {
	t.Helper()
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(line, ":global mbN ") {
			return strings.Trim(strings.TrimPrefix(line, ":global mbN "), `"`)
		}
	}
	t.Fatalf("could not find mbN nonce in script:\n%s", script)
	return ""
}

type fakeMACTelnetServer struct {
	conn       *net.UDPConn
	mac        [6]byte
	username   string
	password   string
	salt       [16]byte
	serverPriv [32]byte
	certFP     string

	firstLoginPrompts bool
	duplicateDataOnce bool

	mu         sync.Mutex
	addrSet    bool
	certSet    bool
	sslOn      bool
	addrAdds   int
	certAdds   int
	loginStage int
	vars       map[string]string
}

func startFakeMACTelnetServer(t *testing.T, user, pass, certFP string) *fakeMACTelnetServer {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen fake mac-telnet udp: %v", err)
	}
	var salt [16]byte
	copy(salt[:], []byte("0123456789abcdef"))
	var srvPriv [32]byte
	srvPriv[31] = 0x55

	s := &fakeMACTelnetServer{
		conn:       conn,
		mac:        [6]byte{0x54, 0x05, 0xab, 0x01, 0x02, 0x03},
		username:   user,
		password:   pass,
		salt:       salt,
		serverPriv: srvPriv,
		certFP:     certFP,
		vars:       map[string]string{},
	}
	go s.serve()
	t.Cleanup(func() { _ = conn.Close() })
	return s
}

func (s *fakeMACTelnetServer) serve() {
	var (
		rxCounter uint32
		txCounter uint32
		clientPub [32]byte
		clientPar byte
		authed    bool
		lineBuf   bytes.Buffer
	)

	buf := make([]byte, 4096)
	for {
		n, raddr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		pkt, err := DecodeMACTelnetPacket(buf[:n], false, 0)
		if err != nil {
			continue
		}

		sendToClient := func(ptype uint8, counter uint32, payload []byte) {
			frame := EncodeMACTelnetPacket(false, ptype, s.mac, pkt.SrcMAC, pkt.SesKey, macTelnetClientType, counter, payload)
			_, _ = s.conn.WriteToUDP(frame, raddr)
		}

		switch pkt.PType {
		case macTelnetPTypeStartSession:
			rxCounter = 0
			txCounter = 0
			authed = false
			lineBuf.Reset()
			sendToClient(macTelnetPTypeAck, 0, nil)
		case macTelnetPTypeEndSession:
			rxCounter = 0
			txCounter = 0
			authed = false
			lineBuf.Reset()
			continue
		case macTelnetPTypePing:
			sendToClient(macTelnetPTypePong, pkt.Counter, pkt.Payload)
		case macTelnetPTypeData:
			if pkt.Counter != rxCounter {
				sendToClient(macTelnetPTypeAck, rxCounter, nil)
				continue
			}
			rxCounter += uint32(len(pkt.Payload))
			sendToClient(macTelnetPTypeAck, rxCounter, nil)

			ctrls, plain, err := ParseDataPayload(pkt.Payload)
			if err != nil {
				continue
			}
			for _, cp := range ctrls {
				switch cp.Type {
				case macTelnetCPEncryptionKey:
					nul := bytes.IndexByte(cp.Data, 0)
					if nul >= 0 && len(cp.Data) == nul+1+33 {
						copy(clientPub[:], cp.Data[nul+1:nul+33])
						clientPar = cp.Data[nul+33]
						srvPub, srvPar, _, err := ServerChallengeAndVerify(s.username, s.password, s.serverPriv, s.salt, clientPub, clientPar, [32]byte{})
						if err == nil {
							var resp [49]byte
							copy(resp[0:32], srvPub[:])
							resp[32] = srvPar
							copy(resp[33:49], s.salt[:])
							pl := EncodeControlPacket(macTelnetCPEncryptionKey, resp[:])
							sendToClient(macTelnetPTypeData, txCounter, pl)
							txCounter += uint32(len(pl))
						}
					}
				case macTelnetCPPassword:
					if len(cp.Data) == 32 {
						var confirm [32]byte
						copy(confirm[:], cp.Data)
						_, _, ok, err := ServerChallengeAndVerify(s.username, s.password, s.serverPriv, s.salt, clientPub, clientPar, confirm)
						if err == nil && ok {
							var out bytes.Buffer
							out.Write(EncodeControlPacket(macTelnetCPEndAuth, nil))
							if s.firstLoginPrompts {
								s.loginStage = 1
								out.WriteString("\r\nMikroTik v7.25.2\r\nDo you want to see the software license? [Y/n]: ")
							} else {
								authed = true
								out.WriteString("\r\nMikroTik v7.25.2\r\n[admin@MikroTik] > ")
							}
							pl := out.Bytes()
							sendToClient(macTelnetPTypeData, txCounter, pl)
							txCounter += uint32(len(pl))
						} else {
							pl := []byte("\r\nLogin failed, incorrect username or password\r\n")
							sendToClient(macTelnetPTypeData, txCounter, pl)
							txCounter += uint32(len(pl))
						}
					}
				}
			}
			if !authed && len(plain) > 0 {
				switch s.loginStage {
				case 1:
					if bytes.Contains(plain, []byte("n")) {
						s.loginStage = 2
						pl := []byte("\r\nChange your password\r\nnew password> ")
						sendToClient(macTelnetPTypeData, txCounter, pl)
						txCounter += uint32(len(pl))
					}
				case 2:
					if bytes.Contains(plain, []byte{0x03}) {
						s.loginStage = 0
						authed = true
						pl := []byte("\r\n[admin@MikroTik] > ")
						sendToClient(macTelnetPTypeData, txCounter, pl)
						txCounter += uint32(len(pl))
					}
				}
				continue
			}
			if authed && len(plain) > 0 {
				lineBuf.Write(plain)
				for {
					b := lineBuf.Bytes()
					idx := bytes.IndexByte(b, '\r')
					if idx < 0 {
						break
					}
					cmd := strings.TrimSpace(string(b[:idx]))
					rest := append([]byte(nil), b[idx+1:]...)
					lineBuf.Reset()
					lineBuf.Write(rest)

					if s.duplicateDataOnce {
						echo := cmd + "\r\n"
						sendToClient(macTelnetPTypeData, txCounter, []byte(echo))
						txCounter += uint32(len(echo))
						sendToClient(macTelnetPTypeData, txCounter-uint32(len(echo)), []byte(echo))
						prompt := "[admin@MikroTik] > "
						sendToClient(macTelnetPTypeData, txCounter, []byte(prompt))
						txCounter += uint32(len(prompt))
					} else {
						respText := s.execFakeCmd(cmd) + "\r\n[admin@MikroTik] > "
						sendToClient(macTelnetPTypeData, txCounter, []byte(respText))
						txCounter += uint32(len(respText))
					}
				}
			}
		}
	}
}

func (s *fakeMACTelnetServer) execFakeCmd(cmd string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case strings.HasPrefix(cmd, ":global "):
		fields := strings.SplitN(strings.TrimPrefix(cmd, ":global "), " ", 2)
		if len(fields) == 2 {
			s.vars[fields[0]] = strings.Trim(fields[1], `"`)
		}
		return cmd
	case strings.Contains(cmd, "/ip/address/add"):
		if !s.addrSet {
			s.addrSet = true
			s.addrAdds++
		}
		return cmd
	case strings.Contains(cmd, "/certificate/add"):
		if !s.certSet {
			s.certSet = true
			s.certAdds++
		}
		return cmd
	case strings.Contains(cmd, "/ip/service/set"):
		s.sslOn = true
		return cmd
	case strings.Contains(cmd, `===MACBOOT-" . "BEGIN-`):
		return cmd + "\r\n===MACBOOT-BEGIN-" + s.vars["mbN"] + "==="
	case strings.Contains(cmd, `===MACBOOT-" . "END-`):
		return cmd + "\r\n===MACBOOT-END-" + s.vars["mbN"] + "==="
	case strings.Contains(cmd, `"fingerprint="`):
		return cmd + "\r\nfingerprint=" + s.certFP
	case strings.Contains(cmd, `"address="`):
		return cmd + "\r\naddress=" + s.vars["mbA"]
	case strings.Contains(cmd, `"identity="`):
		return cmd + "\r\nidentity=MikroTik"
	case strings.Contains(cmd, `"version="`):
		return cmd + "\r\nversion=7.25.2 (stable)"
	default:
		return cmd
	}
}

func loopbackInterfaceName(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "darwin" {
		return "lo0"
	}
	return "lo"
}

func TestBootstrapIdempotentAndVerifiedOverPinnedTLS(t *testing.T) {
	password := "LabSecret!42"
	restSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "admin" || p != password {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/rest/system/resource" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"version": "7.25.2 (stable)",
		})
	}))
	defer restSrv.Close()

	leafSum := sha256.Sum256(restSrv.Certificate().Raw)
	certFP := strings.ToUpper(hex.EncodeToString(leafSum[:]))
	macSrv := startFakeMACTelnetServer(t, "admin", password, certFP)

	loopIf := loopbackInterfaceName(t)
	runner := func(ctx context.Context, cfg BootstrapConfig, script string) (string, error) {
		sess, err := DialMACTelnet(ctx, MACTelnetOptions{
			Interface:      loopIf,
			TargetMAC:      cfg.MACAddress,
			Username:       cfg.Username,
			Password:       cfg.Password,
			Timeout:        5 * time.Second,
			RemoteAddr:     macSrv.conn.LocalAddr().String(),
			UnsafeRawDebug: cfg.UnsafeRawDebug,
			Logger:         cfg.Logger,
		})
		if err != nil {
			return "", err
		}
		defer sess.Close()
		return sess.Exec(ctx, script)
	}

	// First run (TOFU, no expected fingerprint).
	res1, err := Bootstrap(context.Background(), BootstrapConfig{
		Interface:       loopIf,
		MACAddress:      "54:05:AB:01:02:03",
		Address:         "172.30.99.2/24",
		TargetInterface: "ether2",
		Username:        "admin",
		Password:        password,
		AllowInsecureL2: true,
		Timeout:         5 * time.Second,
		restAddr:        strings.TrimPrefix(restSrv.URL, "https://"),
		sessionRunner:   runner,
	})
	if err != nil {
		t.Fatalf("first Bootstrap failed: %v", err)
	}
	if res1.TLSFingerprint != certFP {
		t.Fatalf("TLSFingerprint = %q, want %q", res1.TLSFingerprint, certFP)
	}
	if res1.Warning == "" {
		t.Fatalf("expected TOFU warning when expected_tls_fingerprint is empty")
	}

	// Second run (idempotent + pinned expected_tls_fingerprint).
	res2, err := Bootstrap(context.Background(), BootstrapConfig{
		Interface:              loopIf,
		MACAddress:             "54:05:AB:01:02:03",
		Address:                "172.30.99.2/24",
		TargetInterface:        "ether2",
		Username:               "admin",
		Password:               password,
		AllowInsecureL2:        true,
		ExpectedTLSFingerprint: "sha256:" + strings.ToLower(certFP),
		Timeout:                5 * time.Second,
		restAddr:               strings.TrimPrefix(restSrv.URL, "https://"),
		sessionRunner:          runner,
	})
	if err != nil {
		t.Fatalf("second Bootstrap failed: %v", err)
	}
	if res2.TLSFingerprint != res1.TLSFingerprint {
		t.Fatalf("certificate rotated unexpectedly on second run")
	}
	if res2.Warning != "" {
		t.Fatalf("expected no TOFU warning when expected_tls_fingerprint is pinned, got %q", res2.Warning)
	}
}

func TestDebugRedaction(t *testing.T) {
	password := "UltraSecretPass999"
	macSrv := startFakeMACTelnetServer(t, "admin", password, strings.Repeat("AB", 32))
	loopIf := loopbackInterfaceName(t)

	var safeLogs []string
	sess, err := DialMACTelnet(context.Background(), MACTelnetOptions{
		Interface:      loopIf,
		TargetMAC:      "54:05:AB:01:02:03",
		Username:       "admin",
		Password:       password,
		Timeout:        5 * time.Second,
		RemoteAddr:     macSrv.conn.LocalAddr().String(),
		UnsafeRawDebug: false,
		Logger: func(format string, args ...any) {
			safeLogs = append(safeLogs, fmt.Sprintf(format, args...))
		},
	})
	if err != nil {
		t.Fatalf("DialMACTelnet (safe debug) failed: %v", err)
	}
	_ = sess.Close()

	joinedSafe := strings.Join(safeLogs, "\n")
	if strings.Contains(joinedSafe, password) {
		t.Fatalf("password leaked into safe logs: %s", joinedSafe)
	}
	if strings.Contains(joinedSafe, "raw tx") || strings.Contains(joinedSafe, "raw rx") {
		t.Fatalf("raw hex packet dumps emitted when UnsafeRawDebug is false: %s", joinedSafe)
	}

	var rawLogs []string
	sess2, err := DialMACTelnet(context.Background(), MACTelnetOptions{
		Interface:      loopIf,
		TargetMAC:      "54:05:AB:01:02:03",
		Username:       "admin",
		Password:       password,
		Timeout:        5 * time.Second,
		RemoteAddr:     macSrv.conn.LocalAddr().String(),
		UnsafeRawDebug: true,
		Logger: func(format string, args ...any) {
			rawLogs = append(rawLogs, fmt.Sprintf(format, args...))
		},
	})
	if err != nil {
		t.Fatalf("DialMACTelnet (raw debug) failed: %v", err)
	}
	_ = sess2.Close()

	joinedRaw := strings.Join(rawLogs, "\n")
	if strings.Contains(joinedRaw, password) {
		t.Fatalf("plaintext password leaked into raw logs: %s", joinedRaw)
	}
	if !strings.Contains(joinedRaw, "raw tx") {
		t.Fatalf("expected raw tx hex dumps when UnsafeRawDebug is true, got: %s", joinedRaw)
	}
}

func TestMACTelnetFirstLoginPrompts(t *testing.T) {
	macSrv := startFakeMACTelnetServer(t, "admin", "", strings.Repeat("AB", 32))
	macSrv.firstLoginPrompts = true
	loopIf := loopbackInterfaceName(t)

	sess, err := DialMACTelnet(context.Background(), MACTelnetOptions{
		Interface:  loopIf,
		TargetMAC:  "54:05:AB:01:02:03",
		Username:   "admin",
		Password:   "",
		Timeout:    5 * time.Second,
		RemoteAddr: macSrv.conn.LocalAddr().String(),
	})
	if err != nil {
		t.Fatalf("DialMACTelnet with first-login prompts failed: %v", err)
	}
	_ = sess.Close()
}

func TestMACTelnetIgnoresDuplicateRetransmittedData(t *testing.T) {
	macSrv := startFakeMACTelnetServer(t, "admin", "", strings.Repeat("AB", 32))
	macSrv.duplicateDataOnce = true
	loopIf := loopbackInterfaceName(t)

	sess, err := DialMACTelnet(context.Background(), MACTelnetOptions{
		Interface:  loopIf,
		TargetMAC:  "54:05:AB:01:02:03",
		Username:   "admin",
		Password:   "",
		Timeout:    5 * time.Second,
		RemoteAddr: macSrv.conn.LocalAddr().String(),
	})
	if err != nil {
		t.Fatalf("DialMACTelnet failed: %v", err)
	}
	defer func() { _ = sess.Close() }()

	const cmd = `:set mbA; :set mbC; :set mbI; :set mbN`
	out, err := sess.Exec(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Exec with duplicate retransmitted data failed: %v (output=%q)", err, out)
	}
	if count := strings.Count(out, cmd); count != 1 {
		t.Fatalf("expected command once in output, got %d occurrences: %q", count, out)
	}
	if !strings.HasSuffix(out, "[admin@MikroTik] > ") {
		t.Fatalf("expected output to end with RouterOS prompt, got %q", out)
	}
}
