package routeros

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestResourceUserManagerUserSchema(t *testing.T) {
	if err := Provider().InternalValidate(); err != nil {
		t.Fatalf("Provider().InternalValidate() failed: %v", err)
	}

	r := ResourceUserManagerUser()
	f, ok := r.Schema["shared_users"]
	if !ok {
		t.Fatal("shared_users attribute missing from ResourceUserManagerUser schema")
	}
	if f.Type != schema.TypeString {
		t.Errorf("shared_users Type = %v, want %v", f.Type, schema.TypeString)
	}
	if !f.Optional {
		t.Errorf("shared_users Optional = %v, want true", f.Optional)
	}
	if f.Default != "1" {
		t.Errorf("shared_users Default = %#v (%T), want %q", f.Default, f.Default, "1")
	}
}

func TestResourceUserManagerUserSharedUsersSerialization(t *testing.T) {
	r := ResourceUserManagerUser()

	t.Run("to mikrotik", func(t *testing.T) {
		tests := []struct {
			name string
			set  map[string]cty.Value
			want string
		}{
			{
				name: "omitted uses string default",
				set: map[string]cty.Value{
					"name": cty.StringVal("test"),
				},
				want: "1",
			},
			{
				name: "explicit 1",
				set: map[string]cty.Value{
					"name":         cty.StringVal("test"),
					"shared_users": cty.StringVal("1"),
				},
				want: "1",
			},
			{
				name: "explicit 3",
				set: map[string]cty.Value{
					"name":         cty.StringVal("test"),
					"shared_users": cty.StringVal("3"),
				},
				want: "3",
			},
			{
				name: "explicit unlimited",
				set: map[string]cty.Value{
					"name":         cty.StringVal("test"),
					"shared_users": cty.StringVal("unlimited"),
				},
				want: "unlimited",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				cfg := api725ActionConfig(r, tt.set)
				state := &terraform.InstanceState{RawConfig: cfg.CtyValue}
				diff, err := r.Diff(context.Background(), state, cfg, nil)
				if err != nil {
					t.Fatalf("Diff failed: %v", err)
				}
				client := &wifi7Recorder{
					row: MikrotikItem{
						".id":          "*1",
						"name":         "test",
						"shared-users": tt.want,
					},
				}
				_, diags := r.Apply(context.Background(), state, diff, client)
				if diags.HasError() {
					t.Fatalf("Apply failed: %v", diags)
				}
				if len(client.calls) == 0 || client.calls[0].method != crudCreate {
					t.Fatalf("expected crudCreate call, got %#v", client.calls)
				}
				if got := client.calls[0].body["shared-users"]; got != tt.want {
					t.Errorf("item[\"shared-users\"] = %q, want %q", got, tt.want)
				}
			})
		}
	})

	t.Run("from mikrotik", func(t *testing.T) {
		for _, want := range []string{"1", "3", "unlimited"} {
			t.Run(want, func(t *testing.T) {
				d := r.TestResourceData()
				item := MikrotikItem{
					".id":          "*1",
					"name":         "test",
					"shared-users": want,
				}
				diags := MikrotikResourceDataToTerraform(item, r.Schema, d)
				if diags.HasError() {
					t.Fatalf("unexpected error diagnostics for shared-users=%q: %v", want, diags)
				}
				if got := d.Get("shared_users"); got != want {
					t.Errorf("d.Get(\"shared_users\") = %#v, want %q", got, want)
				}
			})
		}
	})
}

func TestResourceUserManagerUserSharedUsersDiff(t *testing.T) {
	r := ResourceUserManagerUser()

	t.Run("plan unlimited from empty state", func(t *testing.T) {
		cfg := api725ActionConfig(r, map[string]cty.Value{
			"name":         cty.StringVal("test"),
			"shared_users": cty.StringVal("unlimited"),
		})
		diff, err := r.Diff(context.Background(), nil, cfg, nil)
		if err != nil {
			t.Fatalf("Diff failed: %v", err)
		}
		if diff == nil || diff.Attributes["shared_users"] == nil {
			t.Fatalf("expected shared_users diff attribute, got %#v", diff)
		}
		if got := diff.Attributes["shared_users"].New; got != "unlimited" {
			t.Errorf("diff shared_users.New = %q, want %q", got, "unlimited")
		}
	})

	t.Run("plan 3 from empty state", func(t *testing.T) {
		cfg := api725ActionConfig(r, map[string]cty.Value{
			"name":         cty.StringVal("test"),
			"shared_users": cty.StringVal("3"),
		})
		diff, err := r.Diff(context.Background(), nil, cfg, nil)
		if err != nil {
			t.Fatalf("Diff failed: %v", err)
		}
		if diff == nil || diff.Attributes["shared_users"] == nil {
			t.Fatalf("expected shared_users diff attribute, got %#v", diff)
		}
		if got := diff.Attributes["shared_users"].New; got != "3" {
			t.Errorf("diff shared_users.New = %q, want %q", got, "3")
		}
	})

	t.Run("plan omitted default 1 from empty state", func(t *testing.T) {
		cfg := api725ActionConfig(r, map[string]cty.Value{
			"name": cty.StringVal("test"),
		})
		diff, err := r.Diff(context.Background(), nil, cfg, nil)
		if err != nil {
			t.Fatalf("Diff failed: %v", err)
		}
		if diff == nil || diff.Attributes["shared_users"] == nil {
			t.Fatalf("expected shared_users diff attribute, got %#v", diff)
		}
		if got := diff.Attributes["shared_users"].New; got != "1" {
			t.Errorf("diff shared_users.New = %q, want %q", got, "1")
		}
	})

	t.Run("noop when state matches unlimited", func(t *testing.T) {
		cfg := api725ActionConfig(r, map[string]cty.Value{
			"name":         cty.StringVal("test"),
			"shared_users": cty.StringVal("unlimited"),
		})
		d := r.TestResourceData()
		d.SetId("*1")
		if err := d.Set("name", "test"); err != nil {
			t.Fatal(err)
		}
		if err := d.Set("shared_users", "unlimited"); err != nil {
			t.Fatal(err)
		}
		state := d.State()
		state.RawConfig = cfg.CtyValue

		diff, err := r.Diff(context.Background(), state, cfg, nil)
		if err != nil {
			t.Fatalf("Diff failed: %v", err)
		}
		if diff != nil && !diff.Empty() {
			t.Fatalf("expected empty diff, got %#v", diff.Attributes)
		}
	})

	t.Run("in-place update 3 to unlimited", func(t *testing.T) {
		cfg := api725ActionConfig(r, map[string]cty.Value{
			"name":         cty.StringVal("test"),
			"shared_users": cty.StringVal("unlimited"),
		})
		d := r.TestResourceData()
		d.SetId("*1")
		if err := d.Set("name", "test"); err != nil {
			t.Fatal(err)
		}
		if err := d.Set("shared_users", "3"); err != nil {
			t.Fatal(err)
		}
		state := d.State()
		state.RawConfig = cfg.CtyValue

		diff, err := r.Diff(context.Background(), state, cfg, nil)
		if err != nil {
			t.Fatalf("Diff failed: %v", err)
		}
		if diff == nil || diff.Attributes["shared_users"] == nil {
			t.Fatalf("expected shared_users diff attribute, got %#v", diff)
		}
		attrDiff := diff.Attributes["shared_users"]
		if attrDiff.RequiresNew {
			t.Errorf("shared_users RequiresNew = true, want false (in-place update)")
		}
		if attrDiff.Old != "3" || attrDiff.New != "unlimited" {
			t.Errorf("diff shared_users = %q -> %q, want \"3\" -> \"unlimited\"", attrDiff.Old, attrDiff.New)
		}
	})
}

func TestResourceUserManagerUserReadImportUnlimited(t *testing.T) {
	r := ResourceUserManagerUser()

	for _, want := range []string{"unlimited", "1", "3"} {
		t.Run("read_"+want, func(t *testing.T) {
			client := &wifi7Recorder{
				row: MikrotikItem{
					".id":          "*1",
					"name":         "test",
					"shared-users": want,
				},
			}
			d := resourceDataWithRawConfig(r, "*1", map[string]cty.Value{
				"name": cty.StringVal("test"),
			})
			diags := r.ReadContext(context.Background(), d, client)
			if diags.HasError() {
				t.Fatalf("ReadContext returned error diagnostics for shared-users=%q: %v", want, diags)
			}
			if got := d.Get("shared_users"); got != want {
				t.Errorf("d.Get(\"shared_users\") = %#v, want %q", got, want)
			}
			if len(client.calls) != 1 || client.calls[0].method != crudRead {
				t.Errorf("expected 1 crudRead call, got %#v", client.calls)
			}
		})
	}

	t.Run("import_by_name_unlimited", func(t *testing.T) {
		client := &wifi7Recorder{
			row: MikrotikItem{
				".id":          "*1",
				"name":         "test",
				"shared-users": "unlimited",
			},
		}
		d := r.TestResourceData()
		d.SetId("test")

		imported, err := r.Importer.StateContext(context.Background(), d, client)
		if err != nil {
			t.Fatalf("Importer.StateContext failed: %v", err)
		}
		if len(imported) != 1 || imported[0].Id() != "*1" {
			t.Fatalf("unexpected imported ResourceData: %#v", imported)
		}
		diags := r.ReadContext(context.Background(), imported[0], client)
		if diags.HasError() {
			t.Fatalf("ReadContext after import failed: %v", diags)
		}
		if got := imported[0].Get("shared_users"); got != "unlimited" {
			t.Errorf("imported shared_users = %#v, want %q", got, "unlimited")
		}
	})
}

func TestAccUserManagerUser_basic(t *testing.T) {
	testAccPreCheck(t)
	if os.Getenv("ROS_USER_MANAGER_TESTS") != "1" {
		t.Skip("set ROS_USER_MANAGER_TESTS=1 on a disposable router with the user-manager package installed")
	}

	const resourceName = "routeros_user_manager_user.test"
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			config := func(sharedUsers string) string {
				return fmt.Sprintf(`%v

resource "routeros_user_manager_user" "test" {
  name         = "tf-acc-um-user"
  password     = "password"
  shared_users = %q
}
`, providerConfig, sharedUsers)
			}

			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/user-manager/user", "routeros_user_manager_user"),
				Steps: []resource.TestStep{
					{
						Config: config("3"),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(resourceName),
							resource.TestCheckResourceAttr(resourceName, "name", "tf-acc-um-user"),
							resource.TestCheckResourceAttr(resourceName, "shared_users", "3"),
						),
					},
					{
						Config: config("unlimited"),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(resourceName),
							resource.TestCheckResourceAttr(resourceName, "shared_users", "unlimited"),
						),
					},
					{
						ResourceName:            resourceName,
						ImportState:             true,
						ImportStateVerify:       true,
						ImportStateVerifyIgnore: []string{"password", "otp_secret"},
					},
				},
			})
		})
	}
}
