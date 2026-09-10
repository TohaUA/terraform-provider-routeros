package routeros

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-routeros/routeros/v3"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

type Client interface {
	GetExtraParams() *ExtraParams
	GetTransport() TransportType
	SendRequest(method crudMethod, url *URL, item MikrotikItem, result interface{}) error
}

type crudMethod int

const (
	crudUnknown crudMethod = iota
	crudCreate
	crudRead
	crudUpdate
	crudDelete
	crudPost
	crudImport
	crudSign
	crudSignViaScep
	crudRemove
	crudRevoke
	crudMove
	crudStart
	crudStop
	crudGenerateKey
)

type ExtraParams struct {
	SuppressSysODelWarn bool
}

// Provider configuration happens many times in one process: the acceptance
// suite configures once per test case, and a long-lived worker reconfigures on
// every run. Two defects met here.
//
// Every configuration used to dial a fresh API session and build a fresh
// http.Transport, and nothing released either -- the only Close in the provider
// was on a response body. One timed-out acceptance run against 7.24 was holding
// 1,109 open API sessions when it died.
//
// And the API client ran go-routeros in asynchronous mode, which registers a
// reply's tag only after the request has gone out. A reply that arrives inside
// that window is discarded and its caller waits for ever
// (go-routeros/routeros#33), while cancelling a request instead tears down the
// reader every other request on the session depends on (#34). The goroutine
// dumps of three wedged runs each show one request blocked on a reply that had
// already been thrown away, with its socket idle.
//
// Sessions are therefore pooled per credential set, run synchronously, lent to
// one request at a time, and bounded by a deadline on the socket. A synchronous
// session has no tag table to race, exclusive use means nothing interleaves on
// it, and a request that overruns costs its own session and nobody else's.
var (
	connectionMu    sync.Mutex
	apiPools        = map[string]*apiPool{}
	restConnections = map[string]*http.Client{}
)

const (
	// apiPoolSize bounds the sessions opened to one router for one credential
	// set. Requests beyond it wait for a session to come back.
	apiPoolSize = 4

	// apiIdleProbeAfter is how long a session may sit idle before it is checked
	// on its way out of the pool. The router may have restarted in the meantime,
	// and a request should not be the thing that finds out.
	apiIdleProbeAfter = 30 * time.Second
)

func boolKey(v bool) string {
	if v {
		return "1"
	}

	return "0"
}

// connectionKey identifies a credential set. Every part is terminated so
// adjacent fields cannot shift into one another, and the result is hashed so the
// registry's keys never spell out a password if they are ever logged or printed.
// The hash does not keep the password out of memory: a pool's dialer holds it to
// log in again, just as ApiClient and RestClient hold it for their lifetime.
func connectionKey(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}

	return hex.EncodeToString(h.Sum(nil))
}

// apiSession is one logged-in API session, lent to a single request at a time.
type apiSession struct {
	conn     net.Conn
	client   *routeros.Client
	lastUsed time.Time
}

// run sends one command and reads its reply, bounded by a deadline on the
// socket, so a reply that never comes is an error rather than a hang. The
// deadline is set afresh on every run, so one left over from an earlier request
// never applies to the next.
func (s *apiSession) run(sentence []string, timeout time.Duration) (*routeros.Reply, error) {
	if err := s.conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}

	return s.client.RunArgs(sentence)
}

func (s *apiSession) close() error {
	return s.client.Close()
}

// sessionUsable reports whether a session can serve another request after the
// one that returned err. A !trap is RouterOS declining the command: the reply
// still ends in !done and the session stays in step. Anything else -- a
// timeout, a dropped socket, !fatal, a reply word the client does not know --
// leaves the session's place in the byte stream unknown, and the next request
// would read someone else's answer.
func sessionUsable(err error) bool {
	if err == nil {
		return true
	}

	var deviceErr *routeros.DeviceError

	return errors.As(err, &deviceErr) && deviceErr.Sentence != nil && deviceErr.Sentence.Word == "!trap"
}

type apiDialer func(timeout time.Duration) (*apiSession, error)

// apiDial returns a dialer that opens and logs in one synchronous session. It
// builds the session on a connection this code owns, rather than going through
// routeros.Dial, so that a deadline can be put on the socket itself.
func apiDial(address, username, password string, useTLS bool, tlsConf *tls.Config) apiDialer {
	return func(timeout time.Duration) (*apiSession, error) {
		dialer := &net.Dialer{Timeout: timeout}

		var conn net.Conn
		var err error

		if useTLS {
			conn, err = tls.DialWithDialer(dialer, "tcp", address, tlsConf)
		} else {
			conn, err = dialer.Dial("tcp", address)
		}
		if err != nil {
			return nil, fmt.Errorf("could not connect to router os: %w", err)
		}

		client, err := routeros.NewClient(conn)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("could not connect to router os: %w", err)
		}

		if err = conn.SetDeadline(time.Now().Add(timeout)); err == nil {
			err = client.Login(username, password)
		}
		if err != nil {
			_ = client.Close()
			return nil, err
		}

		return &apiSession{conn: conn, client: client, lastUsed: time.Now()}, nil
	}
}

// apiPool lends sessions for one credential set. Dialling and probing happen
// outside its lock, so an unreachable router slows the callers waiting on it and
// nobody else.
type apiPool struct {
	dial    apiDialer
	timeout time.Duration
	size    int

	mu   sync.Mutex
	cond *sync.Cond
	idle []*apiSession
	open int
}

func newAPIPool(dial apiDialer, timeout time.Duration, size int) *apiPool {
	p := &apiPool{dial: dial, timeout: timeout, size: size}
	p.cond = sync.NewCond(&p.mu)

	return p
}

// apiPoolFor returns the pool for a credential set, creating it on first use.
// Nothing here touches the network, so holding the global lock costs nothing.
func apiPoolFor(key string, dial apiDialer, timeout time.Duration) *apiPool {
	connectionMu.Lock()
	defer connectionMu.Unlock()

	if p, ok := apiPools[key]; ok {
		return p
	}

	p := newAPIPool(dial, timeout, apiPoolSize)
	apiPools[key] = p

	return p
}

// acquire lends a session: an idle one where it can, a new one while the pool
// is under its size, and otherwise the next one to come back.
func (p *apiPool) acquire() (*apiSession, error) {
	p.mu.Lock()

	for {
		if n := len(p.idle); n > 0 {
			s := p.idle[n-1]
			p.idle = p.idle[:n-1]
			p.mu.Unlock()

			if time.Since(s.lastUsed) < apiIdleProbeAfter || p.alive(s) {
				return s, nil
			}

			p.discard(s)
			p.mu.Lock()

			continue
		}

		if p.open < p.size {
			p.open++
			p.mu.Unlock()

			s, err := p.dial(p.timeout)
			if err != nil {
				p.mu.Lock()
				p.open--
				p.cond.Signal()
				p.mu.Unlock()

				return nil, err
			}

			return s, nil
		}

		p.cond.Wait()
	}
}

// release takes a session back after a request. A session the request left
// unusable is closed instead of being returned.
func (p *apiPool) release(s *apiSession, usable bool) {
	if !usable {
		p.discard(s)
		return
	}

	s.lastUsed = time.Now()

	p.mu.Lock()
	p.idle = append(p.idle, s)
	p.cond.Signal()
	p.mu.Unlock()
}

func (p *apiPool) discard(s *apiSession) {
	_ = s.close()

	p.mu.Lock()
	p.open--
	p.cond.Signal()
	p.mu.Unlock()
}

// alive checks an idle session with a cheap command before it is lent out.
func (p *apiPool) alive(s *apiSession) bool {
	_, err := s.run([]string{"/system/identity/print"}, min(p.timeout, 5*time.Second))

	return sessionUsable(err)
}

// restClient returns the HTTP client for the given credentials, reused for the
// same reason as the API sessions: a new http.Transport per configuration is a
// new idle connection pool per configuration, and nothing closes them.
func restClient(key string, timeout time.Duration, tlsConf *tls.Config) *http.Client {
	connectionMu.Lock()
	defer connectionMu.Unlock()

	if client, ok := restConnections[key]; ok {
		return client
	}

	client := &http.Client{
		// ... By default, CreateContext has a 20 minute timeout ...
		// but MT REST API timeout is in 60 seconds for any operation.
		// Make the timeout smaller so that the lifetime of the context is less than the lifetime of the session.
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: tlsConf,
		},
	}
	restConnections[key] = client

	return client
}

func NewClient(ctx context.Context, d *schema.ResourceData) (interface{}, diag.Diagnostics) {

	tlsConf := tls.Config{
		InsecureSkipVerify: d.Get("insecure").(bool),
	}

	caCertificate := d.Get("ca_certificate").(string)
	if tlsConf.InsecureSkipVerify && caCertificate != "" {
		return nil, diag.Errorf("You have selected mutually exclusive options: " +
			"ca_certificate and insecure connection. Please check the ENV variables and TF files.")
	}

	// Keyed by content rather than by path, so a CA bundle replaced in place gets
	// sessions built on the new trust roots instead of reusing ones built on the old.
	caFingerprint := ""

	if caCertificate != "" {
		if _, err := os.Stat(caCertificate); err != nil {
			ColorizedDebug(ctx, "Failed to read CA file '"+caCertificate+"', error: "+err.Error())
			return nil, diag.FromErr(err)
		}

		certPool := x509.NewCertPool()
		file, err := os.ReadFile(caCertificate)
		if err != nil {
			ColorizedDebug(ctx, "Failed to read CA file '"+caCertificate+"', error: "+err.Error())
			return nil, diag.Errorf("Failed to read CA file '%s', %v", caCertificate, err)
		}
		certPool.AppendCertsFromPEM(file)
		sum := sha256.Sum256(file)
		caFingerprint = hex.EncodeToString(sum[:])
		tlsConf.RootCAs = certPool
	}

	routerUrl, err := url.Parse(d.Get("hosturl").(string))
	if err != nil || routerUrl.Host == "" {
		routerUrl, err = url.Parse("https://" + d.Get("hosturl").(string))
	}
	if err != nil {
		return nil, diag.Diagnostics{
			{
				Severity: diag.Error,
				Summary:  err.Error(),
				Detail:   "Error while parsing the router URL: '" + d.Get("hosturl").(string) + "'",
			},
		}
	}
	routerUrl.Path = strings.TrimSuffix(routerUrl.Path, "/")

	var useTLS = true
	var transport = TransportREST

	// Parse URL.
	switch routerUrl.Scheme {
	case "http":
	case "https":
	case "apis":
		routerUrl.Scheme = ""
		if routerUrl.Port() == "" {
			routerUrl.Host += ":8729"
		}
		transport = TransportAPI
	case "api":
		routerUrl.Scheme = ""
		if routerUrl.Port() == "" {
			routerUrl.Host += ":8728"
		}
		useTLS = false
		transport = TransportAPI
	default:
		panic("[NewClient] wrong transport type: " + routerUrl.Scheme)
	}

	RouterOSVersion = d.Get("routeros_version").(string)
	if RouterOSVersion != "" {
		ColorizedMessage(ctx, INFO, "RouterOS from env: "+RouterOSVersion)
	}

	requestTimeout := time.Duration(d.Get("rest_timeout").(int)) * time.Second

	if transport == TransportAPI {
		api := &ApiClient{
			ctx:       ctx,
			HostURL:   routerUrl.Host,
			Username:  d.Get("username").(string),
			Password:  d.Get("password").(string),
			Transport: TransportAPI,
			extra: &ExtraParams{
				SuppressSysODelWarn: d.Get("suppress_syso_del_warn").(bool),
			},
		}

		api.pool = apiPoolFor(
			connectionKey(api.HostURL, api.Username, api.Password, caFingerprint, "api",
				boolKey(useTLS), boolKey(tlsConf.InsecureSkipVerify), requestTimeout.String()),
			apiDial(api.HostURL, api.Username, api.Password, useTLS, &tlsConf),
			requestTimeout,
		)

		// Fail at configuration, as dialling eagerly always did, rather than on
		// the first resource. An idle session is handed back without dialling.
		session, err := api.pool.acquire()
		if err != nil {
			return nil, diag.FromErr(err)
		}
		api.pool.release(session, true)

		if RouterOSVersion == "" {
			ros, diags := GetRouterOSVersion(api)
			if diags != nil {
				return nil, diags
			}

			RouterOSVersion = ros
			ColorizedMessage(ctx, INFO, "RouterOS: "+RouterOSVersion)
		}

		return api, nil
	}

	rest := &RestClient{
		ctx:       ctx,
		HostURL:   routerUrl.String(),
		Username:  d.Get("username").(string),
		Password:  d.Get("password").(string),
		Transport: TransportREST,
		extra: &ExtraParams{
			SuppressSysODelWarn: d.Get("suppress_syso_del_warn").(bool),
		},
	}

	rest.Client = restClient(
		connectionKey(rest.HostURL, rest.Username, rest.Password, caFingerprint, "rest",
			boolKey(tlsConf.InsecureSkipVerify), requestTimeout.String()),
		requestTimeout, &tlsConf,
	)

	if RouterOSVersion == "" {
		ros, diags := GetRouterOSVersion(rest)
		if diags != nil {
			return nil, diags
		}

		RouterOSVersion = ros
		ColorizedMessage(ctx, INFO, "RouterOS: "+RouterOSVersion)
	}

	return rest, nil
}

type URL struct {
	Path  string   // URL path without '/rest'.
	Query []string // Query values.
}

// GetApiCmd Returns the set of commands for the API client.
func (u *URL) GetApiCmd() []string {
	res := []string{u.Path}
	//if len(u.Query) > 0 && u.Query[len(u.Query) - 1] != "?#|" {
	//	u.Query = append(u.Query, "?#|")
	//}
	return append(res, u.Query...)
}

// GetRestURL Returns the URL for the client
func (u *URL) GetRestURL() string {
	q := strings.Join(u.Query, "&")
	if len(q) > 0 && q[0] != '?' {
		q = "?" + q
	}
	return u.Path + q
}

// EscapeChars peterGo https://groups.google.com/g/golang-nuts/c/NiQiAahnl5E/m/U60Sm1of-_YJ
func EscapeChars(data []byte) []byte {
	var u = []byte(`\u0000`)
	//var u = []byte(`U+0000`)
	var res = make([]byte, 0, len(data))

	for i, ch := range data {
		if ch < 0x20 {
			res = append(res, u...)
			hex.Encode(res[len(res)-2:], data[i:i+1])
			continue
		}
		res = append(res, ch)
	}
	return res
}

// Obtain a version of RouterOS to automatically customize resource schemas.
func GetRouterOSVersion(m interface{}) (string, diag.Diagnostics) {
	res, err := ReadItems(nil, "/system/resource", m.(Client))
	if err != nil {
		return "", diag.FromErr(err)
	}

	// Resource not found.
	if len(*res) == 0 {
		return "", diag.Errorf("RouterOS version not found")
	}

	version, ok := (*res)[0]["version"]
	if !ok {
		return "", diag.Errorf("RouterOS version not found")
	}

	// d.d | d.d.d
	re := regexp.MustCompile(`^(\d+\.){1,2}\d+`)

	if !re.MatchString(version) {
		return "", diag.Errorf("RouterOS version not found")
	}

	return re.FindString(version), nil
}
