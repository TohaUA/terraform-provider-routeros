package routeros

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/go-cty/cty"
)

const certKeyCleanupKeyContent = "key-pem-content"

type certKeyCleanupFile struct {
	id       string
	contents string
}

// certKeyCleanupClient plays the device for a certificate import from inline
// content: it stores each uploaded file under its own ID (*F1, *F2, ... in
// upload order), fails the import of one of them, and records which file IDs
// the handler removed and which /file queries it made. A file the device
// already consumed is gone, so removing it fails the way the router fails for
// an unknown ID and a query for it returns nothing.
type certKeyCleanupClient struct {
	files        map[string]certKeyCleanupFile // file name -> file
	failImport   string                        // contents of the file whose import fails
	noIdOnCreate bool                          // the create response carries no .id (ROS 7.12.x)
	createErr    bool                          // the device writes the file, then the request fails
	failRemove   bool                          // removing an existing file fails and leaves it in place
	consumed     map[string]bool               // file IDs no longer on the device
	removed      []string                      // file IDs the handler asked to remove
	fileQueries  []string                      // queries of crudRead on /file
}

func newCertKeyCleanupClient() *certKeyCleanupClient {
	return &certKeyCleanupClient{files: map[string]certKeyCleanupFile{}, consumed: map[string]bool{}}
}

func (c *certKeyCleanupClient) GetExtraParams() *ExtraParams { return &ExtraParams{} }

func (c *certKeyCleanupClient) GetTransport() TransportType { return TransportREST }

// keyFileID is the ID the device gave the uploaded private key, or "".
func (c *certKeyCleanupClient) keyFileID() string {
	for _, f := range c.files {
		if f.contents == certKeyCleanupKeyContent {
			return f.id
		}
	}
	return ""
}

func (c *certKeyCleanupClient) present(match func(name string, f certKeyCleanupFile) bool) []MikrotikItem {
	res := []MikrotikItem{}
	for name, f := range c.files {
		if !c.consumed[f.id] && match(name, f) {
			res = append(res, MikrotikItem{".id": f.id, "name": name})
		}
	}
	return res
}

func (c *certKeyCleanupClient) SendRequest(method crudMethod, url *URL, item MikrotikItem, result interface{}) error {
	switch {
	case method == crudCreate && url.Path == "/file":
		id := fmt.Sprintf("*F%d", len(c.files)+1)
		c.files[item["name"]] = certKeyCleanupFile{id: id, contents: item["contents"]}
		if c.createErr {
			return errors.New("connection reset by peer")
		}
		if c.noIdOnCreate {
			*result.(*MikrotikItem) = MikrotikItem{}
		} else {
			*result.(*MikrotikItem) = MikrotikItem{".id": id}
		}
	case method == crudImport:
		f := c.files[item["file-name"]]
		if f.contents == c.failImport {
			return errors.New("from RouterOS device: failure: unable to load file")
		}
		// RouterOS removes a file once it has absorbed it (issue #660).
		c.consumed[f.id] = true
	case method == crudRemove && url.Path == "/file/remove":
		id := item[".id"]
		c.removed = append(c.removed, id)
		if c.consumed[id] {
			return errors.New("from RouterOS device: Bad Request: no such item")
		}
		if c.failRemove {
			return errors.New("from RouterOS device: failure: file is busy")
		}
		c.consumed[id] = true
	case method == crudRead && url.Path == "/file":
		q := strings.Join(url.Query, "&")
		c.fileQueries = append(c.fileQueries, q)
		switch {
		case strings.HasPrefix(q, "?.id="):
			id := strings.TrimPrefix(q, "?.id=")
			*result.(*[]MikrotikItem) = c.present(func(_ string, f certKeyCleanupFile) bool { return f.id == id })
		case strings.HasPrefix(q, "?name="):
			name := strings.TrimPrefix(q, "?name=")
			*result.(*[]MikrotikItem) = c.present(func(n string, _ certKeyCleanupFile) bool { return n == name })
		default:
			return fmt.Errorf("unexpected /file query %q", q)
		}
	case method == crudRead:
		*result.(*[]MikrotikItem) = []MikrotikItem{{".id": "*A", "name": "external.crt"}}
	}
	return nil
}

// TestSystemCertificateImportRemovesTheKeyFile is the regression test for a
// private key left on router storage: an import from key_file_content uploads
// the key to a UUID-named file, and RouterOS only removes it after a successful
// import. When the certificate or the key import failed, the key stayed in the
// file list. The handler must remove the key file on every path, and a file the
// router already consumed must not turn a successful import into an error.
func TestSystemCertificateImportRemovesTheKeyFile(t *testing.T) {
	old := RouterOSVersion
	RouterOSVersion = "7.24"
	t.Cleanup(func() { RouterOSVersion = old })

	tests := []struct {
		name string
		// certFromFile imports the certificate from an existing file, so the key
		// is the only upload.
		certFromFile bool
		setup        func(c *certKeyCleanupClient)
		wantErr      bool
		// wantErrText must appear in the returned error: cleanup never replaces it.
		wantErrText string
		// wantKeyLeft is set only where the device refuses the remove; the
		// handler can then only report it.
		wantKeyLeft bool
		// wantQuery is a /file query the handler must make.
		wantQuery func(keyID string) string
	}{
		{
			name:    "key import fails",
			setup:   func(c *certKeyCleanupClient) { c.failImport = certKeyCleanupKeyContent },
			wantErr: true,
		},
		{
			name:    "certificate import fails",
			setup:   func(c *certKeyCleanupClient) { c.failImport = "cert-pem-content" },
			wantErr: true,
		},
		{
			name:      "import succeeds and the router already removed the key",
			setup:     func(c *certKeyCleanupClient) {},
			wantErr:   false,
			wantQuery: func(keyID string) string { return "?.id=" + keyID },
		},
		{
			name:         "key import fails and the remove fails with the key still present",
			certFromFile: true,
			setup: func(c *certKeyCleanupClient) {
				c.failImport = certKeyCleanupKeyContent
				c.failRemove = true
			},
			wantErr:     true,
			wantErrText: "unable to load file",
			wantKeyLeft: true,
			wantQuery:   func(keyID string) string { return "?.id=" + keyID },
		},
		{
			name:         "the device returns no ID for the uploaded key",
			certFromFile: true,
			setup:        func(c *certKeyCleanupClient) { c.noIdOnCreate = true },
			wantErr:      true,
		},
		{
			name:         "the upload fails after the device wrote the key",
			certFromFile: true,
			setup:        func(c *certKeyCleanupClient) { c.createErr = true },
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := ResourceSystemCertificate()
			d := resourceDataWithRawConfig(res, "", map[string]cty.Value{
				KeyName:       cty.StringVal("external.crt"),
				"common_name": cty.StringVal("External Certificate"),
			})
			block := map[string]interface{}{"key_file_content": certKeyCleanupKeyContent}
			if tt.certFromFile {
				block["cert_file_name"] = "external.crt"
			} else {
				block["cert_file_content"] = "cert-pem-content"
			}
			if err := d.Set("import", []interface{}{block}); err != nil {
				t.Fatalf("setting the import block: %v", err)
			}

			client := newCertKeyCleanupClient()
			tt.setup(client)
			diags := res.CreateContext(context.Background(), d, client)
			if diags.HasError() != tt.wantErr {
				t.Fatalf("CreateContext error = %t, want %t: %v", diags.HasError(), tt.wantErr, diags)
			}

			if tt.wantErrText != "" && !strings.Contains(fmt.Sprint(diags), tt.wantErrText) {
				t.Errorf("CreateContext error = %v, want it to contain %q", diags, tt.wantErrText)
			}

			keyID := client.keyFileID()
			if keyID == "" {
				t.Fatalf("the key was never uploaded; files: %v", client.files)
			}

			var keyRemoveSent bool
			for _, id := range client.removed {
				if id == keyID {
					keyRemoveSent = true
				}
			}
			if !keyRemoveSent {
				t.Errorf("no remove was sent for the key file %s; removed: %v", keyID, client.removed)
			}
			if left := !client.consumed[keyID]; left != tt.wantKeyLeft {
				t.Errorf("key file %s still on the router = %t, want %t", keyID, left, tt.wantKeyLeft)
			}
			if tt.wantQuery != nil {
				want := tt.wantQuery(keyID)
				var found bool
				for _, q := range client.fileQueries {
					if q == want {
						found = true
					}
				}
				if !found {
					t.Errorf("no /file query %q was made; queries: %v", want, client.fileQueries)
				}
			}
		})
	}
}
