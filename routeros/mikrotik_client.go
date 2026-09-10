package routeros

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
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
// every run. Each configuration used to dial a fresh API connection, and build
// a fresh http.Transport with its own pool, and nothing ever released either --
// the only Close in the provider is on a response body. RouterOS caps
// concurrent connections, so a long enough process exhausted the device and the
// next connection simply hung, taking whatever operation was in flight with it.
//
// Connections are therefore kept and reused per distinct credential set. That
// matches how the provider already behaves within a single run, where one
// configuration serves every resource concurrently -- Async() exists for
// exactly that -- so this widens the sharing rather than introducing it.
var (
	connectionMu    sync.Mutex
	apiConnections  = map[string]*routeros.Client{}
	restConnections = map[string]*http.Client{}
)

func boolKey(v bool) string {
	if v {
		return "1"
	}

	return "0"
}

// connectionKey identifies a credential set. The password is hashed rather than
// held in a map key so it cannot surface in a dump of provider state.
func connectionKey(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}

	return hex.EncodeToString(h.Sum(nil))
}

// dialAPI returns a live API connection for the given credentials, reusing one
// already open where possible. A cached connection is probed before being
// handed back: the device may have restarted since it was opened, and returning
// a dead connection would be worse than the leak this replaces.
func dialAPI(key, host, username, password string, useTLS bool, tlsConf *tls.Config) (*routeros.Client, error) {
	connectionMu.Lock()
	defer connectionMu.Unlock()

	if client, ok := apiConnections[key]; ok {
		if _, err := client.Run("/system/identity/print"); err == nil {
			return client, nil
		}

		_ = client.Close()
		delete(apiConnections, key)
	}

	var client *routeros.Client
	var err error

	if useTLS {
		client, err = routeros.DialTLS(host, username, password, tlsConf)
	} else {
		client, err = routeros.Dial(host, username, password)
	}
	if err != nil {
		return nil, err
	}

	// The synchronous client has an infinite wait issue
	// when an error occurs while creating multiple resources.
	client.Async()

	apiConnections[key] = client

	return client, nil
}

// restClient returns the HTTP client for the given credentials. Reused for the
// same reason as the API connection: a new http.Transport per configuration
// means a new idle connection pool per configuration, and nothing closes them.
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

		api.Client, err = dialAPI(
			connectionKey(api.HostURL, api.Username, api.Password, caCertificate, "api", boolKey(useTLS)),
			api.HostURL, api.Username, api.Password, useTLS, &tlsConf,
		)
		if err != nil {
			return nil, diag.FromErr(err)
		}

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

	restTimeout := time.Duration(d.Get("rest_timeout").(int)) * time.Second
	rest.Client = restClient(
		connectionKey(rest.HostURL, rest.Username, rest.Password, caCertificate, "rest",
			boolKey(tlsConf.InsecureSkipVerify), restTimeout.String()),
		restTimeout, &tlsConf,
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
