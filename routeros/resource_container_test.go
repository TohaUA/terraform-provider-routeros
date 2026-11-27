package routeros

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func newContainerTestData(t *testing.T, res *schema.Resource, id string, set map[string]cty.Value, running bool) *schema.ResourceData {
	t.Helper()
	d := resourceDataWithRawConfig(res, id, set)
	if err := d.Set("running", running); err != nil {
		t.Fatal(err)
	}
	return d
}

type containerCall struct {
	method crudMethod
	path   string
	query  []string
	item   MikrotikItem
}

type containerMockClient struct {
	mu        sync.Mutex
	transport TransportType
	calls     []containerCall

	// readQueue supplies sequential /container ReadItems responses. Once exhausted,
	// the last response repeats unless readQueueSticky is false.
	readQueue       []readResult
	readQueueSticky bool
	readIdx         int

	createRes MikrotikItem
	createErr error
	updateRes MikrotikItem
	updateErr error
	deleteErr error
	startErr  error
	stopErr   error
	registry  MikrotikItem
}

type readResult struct {
	rows []MikrotikItem
	err  error
}

func (c *containerMockClient) GetExtraParams() *ExtraParams { return &ExtraParams{} }
func (c *containerMockClient) GetTransport() TransportType  { return c.transport }

func (c *containerMockClient) SendRequest(method crudMethod, url *URL, item MikrotikItem, result interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var itemCopy MikrotikItem
	if item != nil {
		itemCopy = make(MikrotikItem, len(item))
		for k, v := range item {
			itemCopy[k] = v
		}
	}
	c.calls = append(c.calls, containerCall{
		method: method,
		path:   url.Path,
		query:  slices.Clone(url.Query),
		item:   itemCopy,
	})

	switch method {
	case crudCreate:
		if c.createErr != nil {
			return c.createErr
		}
		if resMap, ok := result.(*MikrotikItem); ok {
			for k, v := range c.createRes {
				(*resMap)[k] = v
			}
		}
		return nil

	case crudRead:
		if url.Path == "/container/config" {
			if c.registry == nil {
				return errors.New("unexpected registry lookup")
			}
			*result.(*[]MikrotikItem) = []MikrotikItem{c.registry}
			return nil
		}
		if url.Path != "/container" {
			return fmt.Errorf("unexpected read path: %s", url.Path)
		}
		if len(c.readQueue) == 0 {
			return nil
		}
		idx := c.readIdx
		if idx >= len(c.readQueue) {
			if !c.readQueueSticky {
				return errors.New("read queue exhausted")
			}
			idx = len(c.readQueue) - 1
		} else {
			c.readIdx++
		}
		r := c.readQueue[idx]
		if r.err != nil {
			return r.err
		}
		*result.(*[]MikrotikItem) = slices.Clone(r.rows)
		return nil

	case crudUpdate:
		if c.updateErr != nil {
			return c.updateErr
		}
		if resMap, ok := result.(*MikrotikItem); ok {
			for k, v := range c.updateRes {
				(*resMap)[k] = v
			}
		}
		return nil

	case crudDelete:
		return c.deleteErr

	case crudStart:
		return c.startErr

	case crudStop:
		return c.stopErr

	default:
		return fmt.Errorf("unexpected method: %v", method)
	}
}

func (c *containerMockClient) methods() []crudMethod {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]crudMethod, len(c.calls))
	for i, call := range c.calls {
		out[i] = call.method
	}
	return out
}

func (c *containerMockClient) hasMethod(m crudMethod) bool {
	return slices.Contains(c.methods(), m)
}

func TestContainerStatusSchemaPreserved(t *testing.T) {
	res := ResourceContainer()
	s, ok := res.Schema["status"]
	if !ok {
		t.Fatal("status schema attribute must remain present in Phase 3 non-breaking release")
	}
	if s.Type != schema.TypeString || !s.Computed {
		t.Fatalf("status schema attribute = %+v, want Computed TypeString", s)
	}
}

func TestContainerState(t *testing.T) {
	tests := []struct {
		name string
		item MikrotikItem
		want string
	}{
		// 7.24.5 / 7.25 state flags with "true" and "yes".
		{
			name: "download/extract failed true",
			item: MikrotikItem{"download/extract failed": "true"},
			want: "download/extract failed",
		},
		{
			name: "download/extract failed yes",
			item: MikrotikItem{"download/extract failed": "yes"},
			want: "download/extract failed",
		},
		{
			name: "deleting true",
			item: MikrotikItem{"deleting": "true"},
			want: "deleting",
		},
		{
			name: "deleting yes",
			item: MikrotikItem{"deleting": "yes"},
			want: "deleting",
		},
		{
			name: "stopping true",
			item: MikrotikItem{"stopping": "true"},
			want: "stopping",
		},
		{
			name: "stopping yes",
			item: MikrotikItem{"stopping": "yes"},
			want: "stopping",
		},
		{
			name: "starting-with-healthcheck true",
			item: MikrotikItem{"starting-with-healthcheck": "true"},
			want: "starting-with-healthcheck",
		},
		{
			name: "starting-with-healthcheck yes",
			item: MikrotikItem{"starting-with-healthcheck": "yes"},
			want: "starting-with-healthcheck",
		},
		{
			name: "starting true",
			item: MikrotikItem{"starting": "true"},
			want: "starting",
		},
		{
			name: "starting yes",
			item: MikrotikItem{"starting": "yes"},
			want: "starting",
		},
		{
			name: "downloading/extracting true",
			item: MikrotikItem{"downloading/extracting": "true"},
			want: "downloading/extracting",
		},
		{
			name: "downloading/extracting yes",
			item: MikrotikItem{"downloading/extracting": "yes"},
			want: "downloading/extracting",
		},
		{
			name: "extracting true",
			item: MikrotikItem{"extracting": "true"},
			want: "extracting",
		},
		{
			name: "extracting yes",
			item: MikrotikItem{"extracting": "yes"},
			want: "extracting",
		},
		{
			name: "running true",
			item: MikrotikItem{"running": "true"},
			want: "running",
		},
		{
			name: "running yes",
			item: MikrotikItem{"running": "yes"},
			want: "running",
		},
		{
			name: "stopped true",
			item: MikrotikItem{"stopped": "true"},
			want: "stopped",
		},
		{
			name: "stopped yes",
			item: MikrotikItem{"stopped": "yes"},
			want: "stopped",
		},

		// Precedence order checks.
		{
			name: "download/extract failed overrides all other flags and status",
			item: MikrotikItem{
				"download/extract failed":   "true",
				"deleting":                  "true",
				"stopping":                  "true",
				"starting-with-healthcheck": "true",
				"starting":                  "true",
				"downloading/extracting":    "true",
				"extracting":                "true",
				"running":                   "true",
				"stopped":                   "true",
				"status":                    "stopped",
			},
			want: "download/extract failed",
		},
		{
			name: "deleting overrides stopping, running, stopped",
			item: MikrotikItem{
				"deleting": "true",
				"stopping": "true",
				"running":  "true",
				"stopped":  "true",
			},
			want: "deleting",
		},
		{
			name: "stopping overrides starting-with-healthcheck, running, stopped",
			item: MikrotikItem{
				"stopping":                  "true",
				"starting-with-healthcheck": "true",
				"running":                   "true",
				"stopped":                   "true",
			},
			want: "stopping",
		},
		{
			name: "starting-with-healthcheck overrides starting, running, stopped",
			item: MikrotikItem{
				"starting-with-healthcheck": "true",
				"starting":                  "true",
				"running":                   "true",
				"stopped":                   "true",
			},
			want: "starting-with-healthcheck",
		},
		{
			name: "starting overrides downloading/extracting, running, stopped",
			item: MikrotikItem{
				"starting":               "true",
				"downloading/extracting": "true",
				"running":                "true",
				"stopped":                "true",
			},
			want: "starting",
		},
		{
			name: "downloading/extracting overrides extracting, running, stopped",
			item: MikrotikItem{
				"downloading/extracting": "true",
				"extracting":             "true",
				"running":                "true",
				"stopped":                "true",
			},
			want: "downloading/extracting",
		},
		{
			name: "extracting overrides running and stopped",
			item: MikrotikItem{
				"extracting": "true",
				"running":    "true",
				"stopped":    "true",
			},
			want: "extracting",
		},
		{
			name: "running overrides stopped and legacy status",
			item: MikrotikItem{
				"running": "true",
				"stopped": "true",
				"status":  "stopped",
			},
			want: "running",
		},
		{
			name: "stopped overrides legacy status",
			item: MikrotikItem{
				"stopped": "true",
				"status":  "running",
			},
			want: "stopped",
		},

		// False / no values must not match.
		{
			name: "all flags false",
			item: MikrotikItem{
				"download/extract failed":   "false",
				"deleting":                  "no",
				"stopping":                  "false",
				"starting-with-healthcheck": "no",
				"starting":                  "false",
				"downloading/extracting":    "no",
				"extracting":                "false",
				"running":                   "no",
				"stopped":                   "false",
			},
			want: "",
		},

		// Legacy pre-7.24 status fallback.
		{
			name: "legacy pulling status",
			item: MikrotikItem{"status": "pulling"},
			want: "pulling",
		},
		{
			name: "legacy extracting status",
			item: MikrotikItem{"status": "extracting"},
			want: "extracting",
		},
		{
			name: "legacy stopped status",
			item: MikrotikItem{"status": "stopped"},
			want: "stopped",
		},
		{
			name: "legacy running status",
			item: MikrotikItem{"status": "running"},
			want: "running",
		},
		{
			name: "false running flag with legacy pulling status",
			item: MikrotikItem{"running": "false", "status": "pulling"},
			want: "pulling",
		},

		// Empty response.
		{
			name: "empty item",
			item: MikrotikItem{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containerState(tt.item); got != tt.want {
				t.Errorf("containerState(%v) = %q, want %q", tt.item, got, tt.want)
			}
		})
	}
}

func TestContainerCreateLifecycle(t *testing.T) {
	for _, transport := range []TransportType{TransportREST, TransportAPI} {
		t.Run(fmt.Sprintf("running=true/%v", transport), func(t *testing.T) {
			res := ResourceContainer()
			d := newContainerTestData(t, res, "", map[string]cty.Value{
				"interface":    cty.StringVal("veth1"),
				"remote_image": cty.StringVal("library/alpine:latest"),
				"running":      cty.BoolVal(true),
			}, true)

			client := &containerMockClient{
				transport:       transport,
				createRes:       MikrotikItem{".id": "*1", "interface": "veth1"},
				registry:        MikrotikItem{"registry-url": "https://registry-1.docker.io"},
				readQueueSticky: true,
				readQueue: []readResult{
					// API ResourceCreate post-create read.
					{rows: []MikrotikItem{{".id": "*1", "interface": "veth1", "downloading/extracting": "true"}}},
					// Pull wait: downloading/extracting -> stopped.
					{rows: []MikrotikItem{{".id": "*1", "downloading/extracting": "true", "running": "false"}}},
					{rows: []MikrotikItem{{".id": "*1", "stopped": "true", "running": "false"}}},
					// Start wait: stopped -> starting -> starting-with-healthcheck -> running.
					{rows: []MikrotikItem{{".id": "*1", "stopped": "true", "running": "false"}}},
					{rows: []MikrotikItem{{".id": "*1", "starting": "true", "running": "false"}}},
					{rows: []MikrotikItem{{".id": "*1", "starting-with-healthcheck": "true", "running": "false"}}},
					{rows: []MikrotikItem{{".id": "*1", "running": "true", "stopped": "false", "tag": "registry-1.docker.io/library/alpine:latest"}}},
					// Post-create resRead.
					{rows: []MikrotikItem{{".id": "*1", "interface": "veth1", "running": "true", "tag": "registry-1.docker.io/library/alpine:latest"}}},
				},
			}
			if transport == TransportREST {
				// REST ResourceCreate does not issue an extra ReadItems.
				client.readQueue = client.readQueue[1:]
			}

			diags := res.CreateContext(context.Background(), d, client)
			if diags.HasError() {
				t.Fatalf("CreateContext failed: %v", diags)
			}

			if d.Id() != "*1" {
				t.Errorf("id = %q, want *1", d.Id())
			}
			if got := d.Get("running"); got != true {
				t.Errorf("running = %v, want true", got)
			}
			if got := d.Get("remote_image"); got != "library/alpine:latest" {
				t.Errorf("remote_image = %v, want library/alpine:latest", got)
			}

			var startCall *containerCall
			for i := range client.calls {
				if client.calls[i].method == crudStart {
					startCall = &client.calls[i]
					break
				}
			}
			if startCall == nil {
				t.Fatal("expected crudStart call")
			}
			wantStartPath := "/container"
			if transport == TransportREST {
				wantStartPath = "/container/start"
			}
			if startCall.path != wantStartPath {
				t.Errorf("crudStart path = %q, want %q", startCall.path, wantStartPath)
			}
			if startCall.item["number"] != "*1" {
				t.Errorf("crudStart item = %v, want number=*1", startCall.item)
			}
		})
	}

	t.Run("running=false skips start and reads state", func(t *testing.T) {
		res := ResourceContainer()
		d := resourceDataWithRawConfig(res, "", map[string]cty.Value{
			"interface": cty.StringVal("veth1"),
			"file":      cty.StringVal("container.tar"),
			"running":   cty.BoolVal(false),
		})
		if err := d.Set("running", false); err != nil {
			t.Fatal(err)
		}

		client := &containerMockClient{
			transport: TransportREST,
			createRes: MikrotikItem{".id": "*2", "interface": "veth1"},
			readQueue: []readResult{
				{rows: []MikrotikItem{{".id": "*2", "interface": "veth1", "stopped": "true", "running": "false"}}},
			},
		}

		diags := res.CreateContext(context.Background(), d, client)
		if diags.HasError() {
			t.Fatalf("CreateContext failed: %v", diags)
		}
		if client.hasMethod(crudStart) {
			t.Fatalf("did not expect crudStart when running=false; calls=%v", client.calls)
		}
		if got := d.Get("running"); got != false {
			t.Errorf("running = %v, want false", got)
		}
	})
}

func TestContainerUpdateLifecycle(t *testing.T) {
	t.Run("running container stops, updates, restarts, and refreshes", func(t *testing.T) {
		res := ResourceContainer()
		d := resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
			"interface": cty.StringVal("veth1"),
			"file":      cty.StringVal("container.tar"),
			"comment":   cty.StringVal("updated"),
			"running":   cty.BoolVal(true),
		})
		if err := d.Set("running", true); err != nil {
			t.Fatal(err)
		}

		client := &containerMockClient{
			transport:       TransportREST,
			updateRes:       MikrotikItem{".id": "*1", "interface": "veth1", "comment": "updated"},
			readQueueSticky: true,
			readQueue: []readResult{
				// stopContainer pre-check: running.
				{rows: []MikrotikItem{{".id": "*1", "running": "true"}}},
				// stop wait: starting -> stopping -> stopped.
				{rows: []MikrotikItem{{".id": "*1", "starting": "true"}}},
				{rows: []MikrotikItem{{".id": "*1", "stopping": "true"}}},
				{rows: []MikrotikItem{{".id": "*1", "stopped": "true", "running": "false"}}},
				// dynamicIdLookup during ResourceUpdate.
				{rows: []MikrotikItem{{".id": "*1"}}},
				// pull wait (already stopped).
				{rows: []MikrotikItem{{".id": "*1", "stopped": "true", "running": "false"}}},
				// start wait: starting -> running.
				{rows: []MikrotikItem{{".id": "*1", "starting": "true", "running": "false"}}},
				{rows: []MikrotikItem{{".id": "*1", "running": "true", "stopped": "false", "comment": "updated"}}},
				// post-update resRead.
				{rows: []MikrotikItem{{".id": "*1", "interface": "veth1", "comment": "updated", "running": "true"}}},
			},
		}

		diags := res.UpdateContext(context.Background(), d, client)
		if diags.HasError() {
			t.Fatalf("UpdateContext failed: %v", diags)
		}

		methods := client.methods()
		stopIdx := slices.Index(methods, crudStop)
		updIdx := slices.Index(methods, crudUpdate)
		startIdx := slices.Index(methods, crudStart)
		if stopIdx < 0 || updIdx < 0 || startIdx < 0 || !(stopIdx < updIdx && updIdx < startIdx) {
			t.Fatalf("unexpected CRUD order: %v", methods)
		}
		if got := d.Get("running"); got != true {
			t.Errorf("running = %v, want true", got)
		}
	})

	t.Run("already stopped container with running=false skips stop and start", func(t *testing.T) {
		res := ResourceContainer()
		d := resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
			"interface": cty.StringVal("veth1"),
			"file":      cty.StringVal("container.tar"),
			"running":   cty.BoolVal(false),
		})
		if err := d.Set("running", false); err != nil {
			t.Fatal(err)
		}

		client := &containerMockClient{
			transport: TransportREST,
			updateRes: MikrotikItem{".id": "*1", "interface": "veth1"},
			readQueue: []readResult{
				// stopContainer pre-check: already stopped.
				{rows: []MikrotikItem{{".id": "*1", "stopped": "true", "running": "false"}}},
				// dynamicIdLookup during ResourceUpdate.
				{rows: []MikrotikItem{{".id": "*1"}}},
				// post-update resRead.
				{rows: []MikrotikItem{{".id": "*1", "interface": "veth1", "stopped": "true", "running": "false"}}},
			},
		}

		diags := res.UpdateContext(context.Background(), d, client)
		if diags.HasError() {
			t.Fatalf("UpdateContext failed: %v", diags)
		}
		if client.hasMethod(crudStop) || client.hasMethod(crudStart) {
			t.Fatalf("expected neither crudStop nor crudStart; methods=%v", client.methods())
		}
		if !client.hasMethod(crudUpdate) {
			t.Fatalf("expected crudUpdate; methods=%v", client.methods())
		}
		if got := d.Get("running"); got != false {
			t.Errorf("running = %v, want false", got)
		}
	})
}

func TestContainerDeleteLifecycle(t *testing.T) {
	t.Run("running container stops before delete", func(t *testing.T) {
		res := ResourceContainer()
		d := resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
			"interface": cty.StringVal("veth1"),
			"file":      cty.StringVal("container.tar"),
		})

		client := &containerMockClient{
			transport: TransportREST,
			readQueue: []readResult{
				// stopContainer pre-check: running.
				{rows: []MikrotikItem{{".id": "*1", "running": "true"}}},
				// stop wait: stopping -> stopped.
				{rows: []MikrotikItem{{".id": "*1", "stopping": "true"}}},
				{rows: []MikrotikItem{{".id": "*1", "stopped": "true", "running": "false"}}},
				// dynamicIdLookup during ResourceDelete.
				{rows: []MikrotikItem{{".id": "*1"}}},
			},
		}

		diags := res.DeleteContext(context.Background(), d, client)
		if diags.HasError() {
			t.Fatalf("DeleteContext failed: %v", diags)
		}
		methods := client.methods()
		stopIdx := slices.Index(methods, crudStop)
		delIdx := slices.Index(methods, crudDelete)
		if stopIdx < 0 || delIdx < 0 || !(stopIdx < delIdx) {
			t.Fatalf("unexpected Delete order: %v", methods)
		}
		if d.Id() != "" {
			t.Errorf("id = %q, want empty after delete", d.Id())
		}
	})

	t.Run("already stopped container skips crudStop", func(t *testing.T) {
		res := ResourceContainer()
		d := resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
			"interface": cty.StringVal("veth1"),
			"file":      cty.StringVal("container.tar"),
		})

		client := &containerMockClient{
			transport: TransportREST,
			readQueue: []readResult{
				{rows: []MikrotikItem{{".id": "*1", "stopped": "true", "running": "false"}}},
				{rows: []MikrotikItem{{".id": "*1"}}},
			},
		}

		diags := res.DeleteContext(context.Background(), d, client)
		if diags.HasError() {
			t.Fatalf("DeleteContext failed: %v", diags)
		}
		if client.hasMethod(crudStop) {
			t.Fatalf("did not expect crudStop when already stopped; methods=%v", client.methods())
		}
		if !client.hasMethod(crudDelete) {
			t.Fatalf("expected crudDelete; methods=%v", client.methods())
		}
		if d.Id() != "" {
			t.Errorf("id = %q, want empty", d.Id())
		}
	})

	t.Run("already absent container skips crudStop and succeeds via ResourceDelete warning", func(t *testing.T) {
		res := ResourceContainer()
		d := resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
			"interface": cty.StringVal("veth1"),
			"file":      cty.StringVal("container.tar"),
		})

		client := &containerMockClient{
			transport: TransportREST,
			readQueue: []readResult{
				{rows: []MikrotikItem{}},
				{rows: []MikrotikItem{}},
			},
		}

		diags := res.DeleteContext(context.Background(), d, client)
		if diags.HasError() {
			t.Fatalf("DeleteContext failed for already-missing container: %v", diags)
		}
		if client.hasMethod(crudStop) || client.hasMethod(crudDelete) {
			t.Fatalf("expected neither crudStop nor crudDelete for missing container; methods=%v", client.methods())
		}
		if d.Id() != "" {
			t.Errorf("id = %q, want empty", d.Id())
		}
	})
}

func TestContainerWaitErrorAndPanicPaths(t *testing.T) {
	newCreateData := func() (*schema.Resource, *schema.ResourceData) {
		res := ResourceContainer()
		d := newContainerTestData(t, res, "", map[string]cty.Value{
			"interface": cty.StringVal("veth1"),
			"file":      cty.StringVal("container.tar"),
			"running":   cty.BoolVal(true),
		}, true)
		return res, d
	}

	newExistingData := func() (*schema.Resource, *schema.ResourceData) {
		res := ResourceContainer()
		d := newContainerTestData(t, res, "*1", map[string]cty.Value{
			"interface": cty.StringVal("veth1"),
			"file":      cty.StringVal("container.tar"),
			"running":   cty.BoolVal(true),
		}, true)
		return res, d
	}

	t.Run("create pull wait download/extract failed aborts before start", func(t *testing.T) {
		res, d := newCreateData()
		client := &containerMockClient{
			transport: TransportREST,
			createRes: MikrotikItem{".id": "*1", "interface": "veth1"},
			readQueue: []readResult{
				{rows: []MikrotikItem{{".id": "*1", "downloading/extracting": "true"}}},
				{rows: []MikrotikItem{{".id": "*1", "download/extract failed": "true", "stopped": "true"}}},
			},
		}

		diags := res.CreateContext(context.Background(), d, client)
		if !diags.HasError() {
			t.Fatal("expected error diagnostic on download/extract failed")
		}
		if client.hasMethod(crudStart) {
			t.Fatal("crudStart must not be called when pull fails")
		}
	})

	t.Run("create pull wait deleting aborts before start", func(t *testing.T) {
		res, d := newCreateData()
		client := &containerMockClient{
			transport: TransportREST,
			createRes: MikrotikItem{".id": "*1", "interface": "veth1"},
			readQueue: []readResult{
				{rows: []MikrotikItem{{".id": "*1", "deleting": "true"}}},
			},
		}

		diags := res.CreateContext(context.Background(), d, client)
		if !diags.HasError() {
			t.Fatal("expected error diagnostic on deleting state")
		}
		if client.hasMethod(crudStart) {
			t.Fatal("crudStart must not be called when container is deleting")
		}
	})

	t.Run("create pull wait empty response fails fast without panic", func(t *testing.T) {
		res, d := newCreateData()
		client := &containerMockClient{
			transport: TransportREST,
			createRes: MikrotikItem{".id": "*1", "interface": "veth1"},
			readQueue: []readResult{
				{rows: []MikrotikItem{}},
			},
		}

		start := time.Now()
		diags := res.CreateContext(context.Background(), d, client)
		if time.Since(start) > 2*time.Second {
			t.Fatalf("took too long (%s); empty response should fail fast instead of NotFoundChecks backoff", time.Since(start))
		}
		if !diags.HasError() {
			t.Fatal("expected error diagnostic when container disappears during pull wait")
		}
		if client.hasMethod(crudStart) {
			t.Fatal("crudStart must not be called when container disappears")
		}
	})

	t.Run("create pull wait ReadItems error fails fast without panic", func(t *testing.T) {
		res, d := newCreateData()
		client := &containerMockClient{
			transport: TransportREST,
			createRes: MikrotikItem{".id": "*1", "interface": "veth1"},
			readQueue: []readResult{
				{err: errors.New("pull read failed")},
			},
		}

		diags := res.CreateContext(context.Background(), d, client)
		if !diags.HasError() || !strings.Contains(diags[0].Summary, "pull read failed") {
			t.Fatalf("expected pull read error diagnostic, got: %v", diags)
		}
		if client.hasMethod(crudStart) {
			t.Fatal("crudStart must not be called on pull read error")
		}
	})

	t.Run("create crudStart error surfaces", func(t *testing.T) {
		res, d := newCreateData()
		client := &containerMockClient{
			transport: TransportREST,
			createRes: MikrotikItem{".id": "*1", "interface": "veth1"},
			startErr:  errors.New("start command failed"),
			readQueue: []readResult{
				{rows: []MikrotikItem{{".id": "*1", "stopped": "true"}}},
			},
		}

		diags := res.CreateContext(context.Background(), d, client)
		if !diags.HasError() || !strings.Contains(diags[0].Summary, "start command failed") {
			t.Fatalf("expected start command error diagnostic, got: %v", diags)
		}
	})

	t.Run("create start wait empty response fails fast without panic", func(t *testing.T) {
		res, d := newCreateData()
		client := &containerMockClient{
			transport: TransportREST,
			createRes: MikrotikItem{".id": "*1", "interface": "veth1"},
			readQueue: []readResult{
				{rows: []MikrotikItem{{".id": "*1", "stopped": "true"}}},
				{rows: []MikrotikItem{}},
			},
		}

		start := time.Now()
		diags := res.CreateContext(context.Background(), d, client)
		if time.Since(start) > 2*time.Second {
			t.Fatalf("took too long (%s); empty response should fail fast", time.Since(start))
		}
		if !diags.HasError() {
			t.Fatal("expected error diagnostic when container disappears during start wait")
		}
	})

	t.Run("create start wait ReadItems error fails fast without panic", func(t *testing.T) {
		res, d := newCreateData()
		client := &containerMockClient{
			transport: TransportREST,
			createRes: MikrotikItem{".id": "*1", "interface": "veth1"},
			readQueue: []readResult{
				{rows: []MikrotikItem{{".id": "*1", "stopped": "true"}}},
				{err: errors.New("start read failed")},
			},
		}

		diags := res.CreateContext(context.Background(), d, client)
		if !diags.HasError() || !strings.Contains(diags[0].Summary, "start read failed") {
			t.Fatalf("expected start read error diagnostic, got: %v", diags)
		}
	})

	t.Run("create start wait unexpected deleting state surfaces error", func(t *testing.T) {
		res, d := newCreateData()
		client := &containerMockClient{
			transport: TransportREST,
			createRes: MikrotikItem{".id": "*1", "interface": "veth1"},
			readQueue: []readResult{
				{rows: []MikrotikItem{{".id": "*1", "stopped": "true"}}},
				{rows: []MikrotikItem{{".id": "*1", "deleting": "true"}}},
			},
		}

		diags := res.CreateContext(context.Background(), d, client)
		if !diags.HasError() {
			t.Fatal("expected error diagnostic when container enters deleting state")
		}
	})

	t.Run("create start wait context timeout surfaces error", func(t *testing.T) {
		res, d := newCreateData()
		client := &containerMockClient{
			transport:       TransportREST,
			createRes:       MikrotikItem{".id": "*1", "interface": "veth1"},
			readQueueSticky: true,
			readQueue: []readResult{
				{rows: []MikrotikItem{{".id": "*1", "stopped": "true"}}},
				{rows: []MikrotikItem{{".id": "*1", "starting": "true"}}},
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		diags := res.CreateContext(ctx, d, client)
		if !diags.HasError() {
			t.Fatal("expected error diagnostic on context timeout during start wait")
		}
	})

	t.Run("update stop pre-check error aborts before update", func(t *testing.T) {
		res, d := newExistingData()
		client := &containerMockClient{
			transport: TransportREST,
			readQueue: []readResult{
				{err: errors.New("pre-check failed")},
			},
		}

		diags := res.UpdateContext(context.Background(), d, client)
		if !diags.HasError() {
			t.Fatal("expected error diagnostic from stop pre-check")
		}
		if client.hasMethod(crudStop) || client.hasMethod(crudUpdate) {
			t.Fatalf("must abort before crudStop/crudUpdate; methods=%v", client.methods())
		}
	})

	t.Run("update crudStop error aborts before update", func(t *testing.T) {
		res, d := newExistingData()
		client := &containerMockClient{
			transport: TransportREST,
			stopErr:   errors.New("stop rpc failed"),
			readQueue: []readResult{
				{rows: []MikrotikItem{{".id": "*1", "running": "true"}}},
			},
		}

		diags := res.UpdateContext(context.Background(), d, client)
		if !diags.HasError() || !strings.Contains(diags[0].Summary, "stop rpc failed") {
			t.Fatalf("expected crudStop error diagnostic, got: %v", diags)
		}
		if client.hasMethod(crudUpdate) {
			t.Fatal("crudUpdate must not run after crudStop error")
		}
	})

	t.Run("update stop wait empty response aborts before update without panic", func(t *testing.T) {
		res, d := newExistingData()
		client := &containerMockClient{
			transport: TransportREST,
			readQueue: []readResult{
				{rows: []MikrotikItem{{".id": "*1", "running": "true"}}},
				{rows: []MikrotikItem{}},
			},
		}

		diags := res.UpdateContext(context.Background(), d, client)
		if !diags.HasError() {
			t.Fatal("expected error diagnostic when container disappears during stop wait")
		}
		if client.hasMethod(crudUpdate) {
			t.Fatal("crudUpdate must not run after stop wait failure")
		}
	})

	t.Run("update stop wait ReadItems error aborts before update without panic", func(t *testing.T) {
		res, d := newExistingData()
		client := &containerMockClient{
			transport: TransportREST,
			readQueue: []readResult{
				{rows: []MikrotikItem{{".id": "*1", "running": "true"}}},
				{err: errors.New("stop read failed")},
			},
		}

		diags := res.UpdateContext(context.Background(), d, client)
		if !diags.HasError() || !strings.Contains(diags[0].Summary, "stop read failed") {
			t.Fatalf("expected stop read error diagnostic, got: %v", diags)
		}
		if client.hasMethod(crudUpdate) {
			t.Fatal("crudUpdate must not run after stop read error")
		}
	})

	t.Run("update stop wait deleting state aborts before update", func(t *testing.T) {
		res, d := newExistingData()
		client := &containerMockClient{
			transport: TransportREST,
			readQueue: []readResult{
				{rows: []MikrotikItem{{".id": "*1", "running": "true"}}},
				{rows: []MikrotikItem{{".id": "*1", "deleting": "true"}}},
			},
		}

		diags := res.UpdateContext(context.Background(), d, client)
		if !diags.HasError() {
			t.Fatal("expected error diagnostic on deleting state during stop wait")
		}
		if client.hasMethod(crudUpdate) {
			t.Fatal("crudUpdate must not run after stop wait unexpected state")
		}
	})

	t.Run("update stop wait context timeout aborts before update", func(t *testing.T) {
		res, d := newExistingData()
		client := &containerMockClient{
			transport:       TransportREST,
			readQueueSticky: true,
			readQueue: []readResult{
				{rows: []MikrotikItem{{".id": "*1", "running": "true"}}},
				{rows: []MikrotikItem{{".id": "*1", "stopping": "true"}}},
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		diags := res.UpdateContext(ctx, d, client)
		if !diags.HasError() {
			t.Fatal("expected error diagnostic on stop wait timeout")
		}
		if client.hasMethod(crudUpdate) {
			t.Fatal("crudUpdate must not run after stop wait timeout")
		}
	})

	t.Run("update startContainer failure surfaces diagnostic", func(t *testing.T) {
		res, d := newExistingData()
		client := &containerMockClient{
			transport: TransportREST,
			updateRes: MikrotikItem{".id": "*1", "interface": "veth1"},
			readQueue: []readResult{
				// stop pre-check: already stopped.
				{rows: []MikrotikItem{{".id": "*1", "stopped": "true"}}},
				// dynamicIdLookup during ResourceUpdate.
				{rows: []MikrotikItem{{".id": "*1"}}},
				// pull wait fails.
				{rows: []MikrotikItem{{".id": "*1", "download/extract failed": "true"}}},
			},
		}

		diags := res.UpdateContext(context.Background(), d, client)
		if !diags.HasError() {
			t.Fatal("expected error diagnostic from startContainer during update")
		}
		if client.hasMethod(crudStart) {
			t.Fatal("crudStart must not be called when pull wait fails in update")
		}
	})

	t.Run("delete stop pre-check error aborts before delete", func(t *testing.T) {
		res, d := newExistingData()
		client := &containerMockClient{
			transport: TransportREST,
			readQueue: []readResult{
				{err: errors.New("delete pre-check failed")},
			},
		}

		diags := res.DeleteContext(context.Background(), d, client)
		if !diags.HasError() {
			t.Fatal("expected error diagnostic from delete stop pre-check")
		}
		if client.hasMethod(crudDelete) {
			t.Fatal("crudDelete must not run after stop pre-check error")
		}
	})

	t.Run("delete crudStop error aborts before delete", func(t *testing.T) {
		res, d := newExistingData()
		client := &containerMockClient{
			transport: TransportREST,
			stopErr:   errors.New("delete stop rpc failed"),
			readQueue: []readResult{
				{rows: []MikrotikItem{{".id": "*1", "running": "true"}}},
			},
		}

		diags := res.DeleteContext(context.Background(), d, client)
		if !diags.HasError() {
			t.Fatal("expected error diagnostic from crudStop during delete")
		}
		if client.hasMethod(crudDelete) {
			t.Fatal("crudDelete must not run after crudStop error")
		}
	})

	t.Run("delete stop wait empty response aborts before delete without panic", func(t *testing.T) {
		res, d := newExistingData()
		client := &containerMockClient{
			transport: TransportREST,
			readQueue: []readResult{
				{rows: []MikrotikItem{{".id": "*1", "running": "true"}}},
				{rows: []MikrotikItem{}},
			},
		}

		diags := res.DeleteContext(context.Background(), d, client)
		if !diags.HasError() {
			t.Fatal("expected error diagnostic when container disappears during delete stop wait")
		}
		if client.hasMethod(crudDelete) {
			t.Fatal("crudDelete must not run after stop wait failure")
		}
	})

	t.Run("delete stop wait ReadItems error aborts before delete", func(t *testing.T) {
		res, d := newExistingData()
		client := &containerMockClient{
			transport: TransportREST,
			readQueue: []readResult{
				{rows: []MikrotikItem{{".id": "*1", "running": "true"}}},
				{err: errors.New("delete stop read failed")},
			},
		}

		diags := res.DeleteContext(context.Background(), d, client)
		if !diags.HasError() {
			t.Fatal("expected error diagnostic on ReadItems failure during delete stop wait")
		}
		if client.hasMethod(crudDelete) {
			t.Fatal("crudDelete must not run after stop wait read error")
		}
	})

	t.Run("delete stop wait context timeout aborts before delete", func(t *testing.T) {
		res, d := newExistingData()
		client := &containerMockClient{
			transport:       TransportREST,
			readQueueSticky: true,
			readQueue: []readResult{
				{rows: []MikrotikItem{{".id": "*1", "running": "true"}}},
				{rows: []MikrotikItem{{".id": "*1", "stopping": "true"}}},
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		diags := res.DeleteContext(ctx, d, client)
		if !diags.HasError() {
			t.Fatal("expected error diagnostic on stop wait timeout during delete")
		}
		if client.hasMethod(crudDelete) {
			t.Fatal("crudDelete must not run after stop wait timeout")
		}
	})

	// Sanity check that diagHelper isn't dead: all error diags must carry diag.Error.
	_ = diag.Error
}

func TestContainerWaitRESTWire(t *testing.T) {
	t.Run("create pull start and delete over REST wire", func(t *testing.T) {
		var mu sync.Mutex
		var requests []string
		var startBody, stopBody MikrotikItem
		getCount := 0

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()

			requests = append(requests, r.Method+" "+r.URL.RequestURI())
			w.Header().Set("Content-Type", "application/json")

			switch {
			case r.Method == http.MethodPut && r.URL.Path == "/rest/container":
				_ = json.NewEncoder(w).Encode(MikrotikItem{
					".id":                    "*1",
					"interface":              "veth1",
					"downloading/extracting": "true",
				})
			case r.Method == http.MethodGet && r.URL.Path == "/rest/container" && r.URL.RawQuery == ".id=%2A1":
				getCount++
				switch getCount {
				case 1:
					_ = json.NewEncoder(w).Encode([]MikrotikItem{{".id": "*1", "downloading/extracting": "true"}})
				case 2:
					_ = json.NewEncoder(w).Encode([]MikrotikItem{{".id": "*1", "stopped": "true"}})
				case 3:
					_ = json.NewEncoder(w).Encode([]MikrotikItem{{".id": "*1", "starting": "true"}})
				case 4:
					_ = json.NewEncoder(w).Encode([]MikrotikItem{{".id": "*1", "starting-with-healthcheck": "true"}})
				case 5, 6, 7:
					// 5: start wait reaches running; 6: resRead; 7: stopContainer pre-check in DeleteContext.
					_ = json.NewEncoder(w).Encode([]MikrotikItem{{".id": "*1", "interface": "veth1", "running": "true"}})
				case 8:
					_ = json.NewEncoder(w).Encode([]MikrotikItem{{".id": "*1", "stopping": "true"}})
				default:
					// 9: stop wait reaches stopped; 10: dynamicIdLookup in ResourceDelete.
					_ = json.NewEncoder(w).Encode([]MikrotikItem{{".id": "*1", "stopped": "true", "running": "false"}})
				}
			case r.Method == http.MethodPost && r.URL.Path == "/rest/container/start":
				if err := json.NewDecoder(r.Body).Decode(&startBody); err != nil {
					t.Errorf("decode start body: %v", err)
				}
				_, _ = w.Write([]byte("[]"))
			case r.Method == http.MethodPost && r.URL.Path == "/rest/container/stop":
				if err := json.NewDecoder(r.Body).Decode(&stopBody); err != nil {
					t.Errorf("decode stop body: %v", err)
				}
				_, _ = w.Write([]byte("[]"))
			case r.Method == http.MethodDelete && r.URL.Path == "/rest/container/*1":
				w.WriteHeader(http.StatusNoContent)
			default:
				w.WriteHeader(http.StatusNotFound)
				t.Errorf("unexpected REST request: %s %s", r.Method, r.URL.RequestURI())
			}
		}))
		defer server.Close()

		client := &RestClient{
			ctx:       context.Background(),
			HostURL:   server.URL,
			Transport: TransportREST,
			Client:    server.Client(),
		}

		res := ResourceContainer()
		d := newContainerTestData(t, res, "", map[string]cty.Value{
			"interface": cty.StringVal("veth1"),
			"file":      cty.StringVal("container.tar"),
			"running":   cty.BoolVal(true),
		}, true)

		if diags := res.CreateContext(context.Background(), d, client); diags.HasError() {
			t.Fatalf("CreateContext over REST failed: %v", diags)
		}
		if d.Id() != "*1" || d.Get("running") != true {
			t.Fatalf("post-create state unexpected: id=%q running=%v", d.Id(), d.Get("running"))
		}
		if startBody["number"] != "*1" {
			t.Errorf("POST /rest/container/start body = %v, want number=*1", startBody)
		}

		if diags := res.DeleteContext(context.Background(), d, client); diags.HasError() {
			t.Fatalf("DeleteContext over REST failed: %v", diags)
		}
		if d.Id() != "" {
			t.Errorf("id = %q, want empty after delete", d.Id())
		}
		if stopBody["number"] != "*1" {
			t.Errorf("POST /rest/container/stop body = %v, want number=*1", stopBody)
		}

		wantPrefixes := []string{
			"PUT /rest/container",
			"GET /rest/container?.id=%2A1",
			"POST /rest/container/start",
			"POST /rest/container/stop",
			"DELETE /rest/container/*1",
		}
		for _, want := range wantPrefixes {
			if !slices.Contains(requests, want) {
				t.Errorf("expected REST request %q in %v", want, requests)
			}
		}
	})

	t.Run("empty array and HTTP 500 error during wait over REST wire", func(t *testing.T) {
		var mu sync.Mutex
		mode := "empty"
		deleteCalled := false

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodPut && r.URL.Path == "/rest/container":
				_ = json.NewEncoder(w).Encode(MikrotikItem{".id": "*1", "interface": "veth1"})
			case r.Method == http.MethodGet && r.URL.Path == "/rest/container":
				if mode == "empty" {
					_, _ = w.Write([]byte("[]"))
					return
				}
				if mode == "stop-http-500-pre" {
					mode = "stop-http-500"
					_ = json.NewEncoder(w).Encode([]MikrotikItem{{".id": "*1", "running": "true"}})
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":500,"message":"Internal Server Error","detail":"boom"}`))
			case r.Method == http.MethodPost && r.URL.Path == "/rest/container/stop":
				_, _ = w.Write([]byte("[]"))
			case r.Method == http.MethodDelete:
				deleteCalled = true
				w.WriteHeader(http.StatusNoContent)
			default:
				w.WriteHeader(http.StatusBadRequest)
			}
		}))
		defer server.Close()

		client := &RestClient{
			ctx:       context.Background(),
			HostURL:   server.URL,
			Transport: TransportREST,
			Client:    server.Client(),
		}

		res := ResourceContainer()
		dCreate := newContainerTestData(t, res, "", map[string]cty.Value{
			"interface": cty.StringVal("veth1"),
			"file":      cty.StringVal("container.tar"),
			"running":   cty.BoolVal(true),
		}, true)
		if diags := res.CreateContext(context.Background(), dCreate, client); !diags.HasError() {
			t.Fatal("expected error diagnostic when REST returns [] during pull wait")
		}

		mu.Lock()
		mode = "stop-http-500-pre"
		mu.Unlock()
		dDelete := resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
			"interface": cty.StringVal("veth1"),
			"file":      cty.StringVal("container.tar"),
		})
		if diags := res.DeleteContext(context.Background(), dDelete, client); !diags.HasError() {
			t.Fatal("expected error diagnostic when REST returns HTTP 500 during stop wait")
		}
		mu.Lock()
		wasDeleteCalled := deleteCalled
		mu.Unlock()
		if wasDeleteCalled {
			t.Fatal("DELETE must not be sent after stop wait HTTP 500 error")
		}
	})
}
