package routeros

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/hashicorp/go-cty/cty"
)

type containerReadClient struct {
	transport TransportType
	rows      []MikrotikItem
	registry  MikrotikItem
	readErr   error
	paths     []string
}

func (c *containerReadClient) GetExtraParams() *ExtraParams { return &ExtraParams{} }
func (c *containerReadClient) GetTransport() TransportType  { return c.transport }

func (c *containerReadClient) SendRequest(method crudMethod, url *URL, item MikrotikItem, result interface{}) error {
	c.paths = append(c.paths, url.Path)
	if method != crudRead || item != nil {
		return fmt.Errorf("unexpected container request: method=%v path=%s item=%v", method, url.Path, item)
	}
	switch url.Path {
	case "/container":
		if c.readErr != nil {
			return c.readErr
		}
		*result.(*[]MikrotikItem) = c.rows
	case "/container/config":
		if c.registry == nil {
			return errors.New("unexpected registry lookup")
		}
		*result.(*[]MikrotikItem) = []MikrotikItem{c.registry}
	default:
		return fmt.Errorf("unexpected container path %s", url.Path)
	}
	return nil
}

func TestContainerReadRunningState(t *testing.T) {
	for _, transport := range []TransportType{TransportAPI, TransportREST} {
		t.Run(fmt.Sprint(transport), func(t *testing.T) {
			for _, tt := range []struct {
				name          string
				item          MikrotikItem
				registryField string
				wantRunning   bool
			}{
				{"current running with tag", MikrotikItem{"running": "true", "status": "stopped", "tag": "registry-1.docker.io/library/alpine:latest"}, "registry-url", true},
				{"current stopped with tag", MikrotikItem{"running": "false", "status": "running", "tag": "registry-1.docker.io/library/alpine:latest"}, "registry-url", false},
				{"current running without tag", MikrotikItem{"running": "true"}, "", true},
				{"current stopped without tag", MikrotikItem{"running": "false"}, "", false},
				{"current yes without tag", MikrotikItem{"running": "yes"}, "", true},
				{"current no without tag", MikrotikItem{"running": "no"}, "", false},
				{"legacy running without tag", MikrotikItem{"status": "running"}, "", true},
				{"legacy stopped with tag", MikrotikItem{"status": "stopped", "tag": "registry-1.docker.io/library/alpine:latest"}, "assumed-registry-url", false},
				{"no running field or status", MikrotikItem{}, "", false},
			} {
				t.Run(tt.name, func(t *testing.T) {
					res := ResourceContainer()
					d := resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
						"interface": cty.StringVal("veth1"),
						"file":      cty.StringVal("container.tar"),
					})
					// A refresh must replace stale state in either direction.
					if err := d.Set("running", !tt.wantRunning); err != nil {
						t.Fatal(err)
					}
					item := MikrotikItem{".id": "*1", "name": "test-container", "interface": "veth1"}
					for key, value := range tt.item {
						item[key] = value
					}
					client := &containerReadClient{transport: transport, rows: []MikrotikItem{item}}
					if tt.registryField != "" {
						client.registry = MikrotikItem{tt.registryField: "https://registry-1.docker.io"}
					}
					if diags := res.ReadContext(context.Background(), d, client); diags.HasError() {
						t.Fatalf("container refresh: %v", diags)
					}
					if got := d.Get("running"); got != tt.wantRunning {
						t.Errorf("running = %v, want %v; response=%v", got, tt.wantRunning, item)
					}
					if d.Id() != "*1" || d.Get("name") != "test-container" {
						t.Errorf("container identity was not refreshed: id=%q name=%v", d.Id(), d.Get("name"))
					}
					wantReads := 1
					if tt.registryField != "" {
						wantReads++
						if got := d.Get("remote_image"); got != "library/alpine:latest" {
							t.Errorf("remote_image = %v, want library/alpine:latest", got)
						}
					}
					if len(client.paths) != wantReads {
						t.Errorf("read paths = %v, want %d reads", client.paths, wantReads)
					}
				})
			}
		})
	}
}

func TestContainerReadMissingAndFailed(t *testing.T) {
	for _, tt := range []struct {
		name    string
		readErr error
		wantID  string
	}{
		{"missing clears state without registry lookup", nil, ""},
		{"request failure preserves identity", errors.New("router unavailable"), "*1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := ResourceContainer()
			d := resourceDataWithRawConfig(res, "*1", nil)
			if err := d.Set("tag", "stale-image-tag"); err != nil {
				t.Fatal(err)
			}
			client := &containerReadClient{transport: TransportREST, readErr: tt.readErr}
			diags := res.ReadContext(context.Background(), d, client)
			if diags.HasError() != (tt.readErr != nil) {
				t.Errorf("refresh diagnostics = %v, want error=%t", diags, tt.readErr != nil)
			}
			if d.Id() != tt.wantID {
				t.Errorf("id = %q, want %q", d.Id(), tt.wantID)
			}
			if len(client.paths) != 1 || client.paths[0] != "/container" {
				t.Errorf("unexpected follow-up request after missing/failed read: %v", client.paths)
			}
		})
	}
}

func TestContainerRunningIsNotSerialized(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(fmt.Sprint(running), func(t *testing.T) {
			res := ResourceContainer()
			d := resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
				"interface": cty.StringVal("veth1"),
				"file":      cty.StringVal("container.tar"),
				"running":   cty.BoolVal(running),
			})
			if err := d.Set("running", running); err != nil {
				t.Fatal(err)
			}
			item, _ := TerraformResourceDataToMikrotik(res.Schema, d)
			if _, sent := item["running"]; sent {
				t.Errorf("running was sent as a container property: %v", item)
			}
			if item["interface"] != "veth1" || item["file"] != "container.tar" {
				t.Errorf("declared container settings were not serialized: %v", item)
			}
		})
	}
}
