package routeros

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/go-cty/cty"
)

// certImportClient plays the device for a certificate import: it accepts every
// command, answers every read with the imported certificate and records the
// requests, so a test can see what the create handler sent.
type certImportClient struct {
	requests []certImportRequest
}

type certImportRequest struct {
	method crudMethod
	path   string
	item   MikrotikItem
}

func (c *certImportClient) GetExtraParams() *ExtraParams { return &ExtraParams{} }

func (c *certImportClient) GetTransport() TransportType { return TransportREST }

func (c *certImportClient) SendRequest(method crudMethod, url *URL, item MikrotikItem, result interface{}) error {
	c.requests = append(c.requests, certImportRequest{method: method, path: url.Path, item: item})
	if method == crudRead {
		*result.(*[]MikrotikItem) = []MikrotikItem{{".id": "*A", "name": "external.crt", "trust-store": "all"}}
	}
	return nil
}

// TestSystemCertificateImportAppliesSettings is the regression test for an import
// that ignored trust_store and trusted: the import command only takes the file and
// its passphrase, so the certificate kept trust-store=all until a second apply.
// The settings the configuration declares must be set right after the import, and
// nothing else: the ForceNew template fields such as common_name cannot be set on
// an imported certificate, and a configuration that declares no settings keeps the
// import-only request sequence.
func TestSystemCertificateImportAppliesSettings(t *testing.T) {
	old := RouterOSVersion
	RouterOSVersion = "7.24"
	t.Cleanup(func() { RouterOSVersion = old })

	tests := []struct {
		name    string
		config  map[string]cty.Value
		trusted bool
		want    MikrotikItem // nil: no settings request at all
	}{
		{
			name:   "declared trust_store is set after the import",
			config: map[string]cty.Value{"trust_store": cty.StringVal("www")},
			want:   MikrotikItem{"trust-store": "www"},
		},
		{
			name:    "declared trust_store and trusted are set after the import",
			config:  map[string]cty.Value{"trust_store": cty.StringVal("www"), "trusted": cty.True},
			trusted: true,
			want:    MikrotikItem{"trust-store": "www", "trusted": BoolToMikrotikJSON(true)},
		},
		{
			name:   "no declared settings sends only the import",
			config: map[string]cty.Value{},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := map[string]cty.Value{
				KeyName:       cty.StringVal("external.crt"),
				"common_name": cty.StringVal("External Certificate"),
			}
			for k, v := range tt.config {
				config[k] = v
			}

			res := ResourceSystemCertificate()
			d := resourceDataWithRawConfig(res, "", config)
			if err := d.Set("import", []interface{}{map[string]interface{}{"cert_file_name": "external.crt"}}); err != nil {
				t.Fatalf("setting the import block: %v", err)
			}
			if tt.trusted {
				if err := d.Set("trusted", true); err != nil {
					t.Fatalf("setting trusted: %v", err)
				}
			}

			client := &certImportClient{}
			if diags := res.CreateContext(context.Background(), d, client); diags.HasError() {
				t.Fatalf("CreateContext returned %v", diags)
			}

			lastImport, update, lastRead := -1, -1, -1
			var updates []certImportRequest
			for i, r := range client.requests {
				switch r.method {
				case crudImport:
					lastImport = i
				case crudUpdate:
					update = i
					updates = append(updates, r)
				case crudRead:
					lastRead = i
				}
			}

			if lastImport < 0 {
				t.Fatalf("no import request was sent: %+v", client.requests)
			}

			if tt.want == nil {
				if len(updates) != 0 {
					t.Fatalf("expected no settings request, got %+v", updates)
				}
				return
			}

			if len(updates) != 1 {
				t.Fatalf("expected one settings request after the import, got %d: %+v", len(updates), client.requests)
			}
			if got := updates[0].item; !reflect.DeepEqual(got, tt.want) {
				t.Errorf("settings request item = %v, want %v", got, tt.want)
			}
			if got, want := updates[0].path, "/certificate/*A"; got != want {
				t.Errorf("settings request path = %q, want %q", got, want)
			}
			if update < lastImport {
				t.Errorf("settings were sent before the import (update #%d, import #%d)", update, lastImport)
			}
			if lastRead < update {
				t.Errorf("state was not read back after the settings (update #%d, last read #%d)", update, lastRead)
			}
		})
	}
}
