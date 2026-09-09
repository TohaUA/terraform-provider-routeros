package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// Client is a GET-only RouterOS REST client. It never issues any other HTTP method.
type Client struct {
	Base     string // "https://router" (no /rest suffix)
	Username string
	Password string
	HTTP     *http.Client
}

// ErrMenuAbsent is returned when the device does not have the menu at all
// (HTTP 404, or HTTP 400 "no such command or directory" for a package that is not installed).
var ErrMenuAbsent = errors.New("menu absent on device")

// NewClientFromEnv builds a client from ROS_HOSTURL, ROS_USERNAME, ROS_PASSWORD, ROS_CACERT
// (path to a PEM bundle) and ROS_INSECURE ("true" skips certificate verification).
func NewClientFromEnv(timeout time.Duration) (*Client, error) {
	host := strings.TrimSpace(os.Getenv("ROS_HOSTURL"))
	if host == "" {
		return nil, errors.New("ROS_HOSTURL is not set")
	}
	user := os.Getenv("ROS_USERNAME")
	if user == "" {
		return nil, errors.New("ROS_USERNAME is not set")
	}
	tlsConf := &tls.Config{}
	if strings.EqualFold(os.Getenv("ROS_INSECURE"), "true") {
		tlsConf.InsecureSkipVerify = true
	}
	if ca := os.Getenv("ROS_CACERT"); ca != "" {
		pem, err := os.ReadFile(ca)
		if err != nil {
			return nil, fmt.Errorf("ROS_CACERT: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("ROS_CACERT: no certificates found in %s", ca)
		}
		tlsConf.RootCAs = pool
	}
	return &Client{
		Base:     normalizeBase(host),
		Username: user,
		Password: os.Getenv("ROS_PASSWORD"),
		HTTP: &http.Client{
			Timeout:   timeout,
			Transport: &http.Transport{TLSClientConfig: tlsConf},
		},
	}, nil
}

// normalizeBase strips a trailing slash and a trailing "/rest" so that both
// "https://host" and "https://host/rest/" are accepted.
func normalizeBase(host string) string {
	host = strings.TrimRight(host, "/")
	host = strings.TrimSuffix(host, "/rest")
	return strings.TrimRight(host, "/")
}

// Get fetches a menu ("/ip/service") and returns its items. A singleton (settings) menu is
// returned as one item. Values are stringified; RouterOS returns everything as strings anyway.
func (c *Client) Get(menu string) ([]map[string]string, error) {
	req, err := http.NewRequest(http.MethodGet, c.Base+"/rest"+menu, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.Username, c.Password)
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return DecodeMenuResponse(resp.StatusCode, body)
}

var reNoSuchCommand = regexp.MustCompile(`no such command`)

// DecodeMenuResponse turns a REST GET response into items.
func DecodeMenuResponse(status int, body []byte) ([]map[string]string, error) {
	switch {
	case status == http.StatusOK:
	case status == http.StatusNotFound:
		return nil, fmt.Errorf("%w: HTTP 404", ErrMenuAbsent)
	case status == http.StatusBadRequest && reNoSuchCommand.Match(body):
		return nil, fmt.Errorf("%w: %s", ErrMenuAbsent, errorDetail(body))
	default:
		return nil, fmt.Errorf("HTTP %d: %s", status, errorDetail(body))
	}

	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var items []map[string]any
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, fmt.Errorf("decode list: %w", err)
		}
		out := make([]map[string]string, 0, len(items))
		for _, it := range items {
			out = append(out, stringify(it))
		}
		return out, nil
	}
	var item map[string]any
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, fmt.Errorf("decode object: %w", err)
	}
	return []map[string]string{stringify(item)}, nil
}

func stringify(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = s
		} else {
			out[k] = fmt.Sprint(v)
		}
	}
	return out
}

func errorDetail(body []byte) string {
	var e struct {
		Detail  string `json:"detail"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil && (e.Detail != "" || e.Message != "") {
		if e.Detail != "" {
			return e.Detail
		}
		return e.Message
	}
	s := strings.TrimSpace(string(body))
	if len(s) > 120 {
		s = s[:120] + "..."
	}
	return s
}

var reVersion = regexp.MustCompile(`^(\d+\.){1,2}\d+`)

// Version returns the RouterOS version of the device ("7.24") from /system/resource.
func (c *Client) Version() (string, error) {
	items, err := c.Get("/system/resource")
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", errors.New("/system/resource returned no items")
	}
	return ParseVersion(items[0]["version"])
}

// ParseVersion extracts "7.24" from "7.24 (stable)".
func ParseVersion(s string) (string, error) {
	v := reVersion.FindString(strings.TrimSpace(s))
	if v == "" {
		return "", fmt.Errorf("cannot parse RouterOS version from %q", s)
	}
	return v, nil
}
