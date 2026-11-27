package routeros

import (
	"context"
	"os"
	"testing"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testUserAddress2 = "routeros_system_user_sshkeys.test"

func TestUserSshKeyTest_basic(t *testing.T) {
	// t.Parallel()
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/user/ssh-keys", "routeros_system_user_sshkeys"),
				Steps: []resource.TestStep{
					{
						Config: testUserSshKeyConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testUserAddress2),
							resource.TestCheckResourceAttr(testUserAddress2, "key_type", "rsa"),
						),
					},
				},
			})

		})
	}
}

func testUserSshKeyConfig() string {
	return providerConfig + `
resource "routeros_system_user" "test" {
  name     = "test-user-1"
  address  = "0.0.0.0/0"
  group    = "read"
  password = "secret"
  comment  = "Test User"
}

resource "routeros_system_user_sshkeys" "test" {
  depends_on  = [routeros_system_user.test]
  user        = "test-user-1"
  key         = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQCyJ1EvW98veNVzR3VamNgmu0xOd/JK9YNvP/pa4WC5eT90UbX4TN7dKEK/x2FCwnnG9u0FQhzG2qa/Cg8meUvlfydn6uxc0/WCeXTKSu6sT63noPO6m4fHY7gu3Zt+fOc/WYGch9sBeWjZlCS1mA2lajkWhM3J8TFWCFm2Zk4/S3s5mt6VLbwpQnH2LhE41+azzDEVhcR6i3FfdgOF/J+j2fYYHJsBEKoQA5zUac2zWmz7X4Rv/g11ZBRqdMpHSD58o5F9lBb13antu5GcEs5RXpXp08OyXuRV9qhFpDBC8DOMALSOgT3vnu8uJLgo8QIulERofj/cRXbLCsmvMbpioBuGFXWx3ha4Ntd6z07kUh2KVbaIQLd/629UHNvgIhoBLlREJ8E5vllsX+jh8hRITHcCiEwXcDO+gG3hvJt0+jm8S8SObE/IHk8VuwWdhIsSku5vd+wVlxm8VeJzjc0cjdIiytvsq8VpLudKEUiqR0f2tHcoq8H+xcJv3Ycu1i8="
  comment     = "Test User"
}
`
}

func TestAccUserSshKey_info725(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("requires TF_ACC and a disposable RouterOS instance")
	}

	const keyAddr = "routeros_system_user_sshkeys.test_info"
	const pubKey = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQCyJ1EvW98veNVzR3VamNgmu0xOd/JK9YNvP/pa4WC5eT90UbX4TN7dKEK/x2FCwnnG9u0FQhzG2qa/Cg8meUvlfydn6uxc0/WCeXTKSu6sT63noPO6m4fHY7gu3Zt+fOc/WYGch9sBeWjZlCS1mA2lajkWhM3J8TFWCFm2Zk4/S3s5mt6VLbwpQnH2LhE41+azzDEVhcR6i3FfdgOF/J+j2fYYHJsBEKoQA5zUac2zWmz7X4Rv/g11ZBRqdMpHSD58o5F9lBb13antu5GcEs5RXpXp08OyXuRV9qhFpDBC8DOMALSOgT3vnu8uJLgo8QIulERofj/cRXbLCsmvMbpioBuGFXWx3ha4Ntd6z07kUh2KVbaIQLd/629UHNvgIhoBLlREJ8E5vllsX+jh8hRITHcCiEwXcDO+gG3hvJt0+jm8S8SObE/IHk8VuwWdhIsSku5vd+wVlxm8VeJzjc0cjdIiytvsq8VpLudKEUiqR0f2tHcoq8H+xcJv3Ycu1i8="

	cfg := func(info string) string {
		return providerConfig + `
resource "routeros_system_user" "test_info" {
  name     = "test-user-info-725"
  address  = "0.0.0.0/0"
  group    = "read"
  password = "secret"
  comment  = "Test User Info 725"
}

resource "routeros_system_user_sshkeys" "test_info" {
  depends_on = [routeros_system_user.test_info]
  user       = routeros_system_user.test_info.name
  key        = "` + pubKey + `"
  info       = "` + info + `"
  comment    = "Test User Info"
}
`
	}

	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/user/ssh-keys", "routeros_system_user_sshkeys"),
				Steps: []resource.TestStep{
					{
						Config: cfg("laptop-key"),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(keyAddr),
							resource.TestCheckResourceAttr(keyAddr, "info", "laptop-key"),
						),
					},
					{
						Config: cfg("workstation-key"),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(keyAddr),
							resource.TestCheckResourceAttr(keyAddr, "info", "workstation-key"),
						),
					},
				},
			})
		})
	}
}

func TestResourceUserSshKeysInfoForceNewAndDiffSuppress(t *testing.T) {
	res := ResourceUserSshKeys()
	infoSchema := res.Schema["info"]
	if infoSchema == nil {
		t.Fatal("missing info field on ResourceUserSshKeys")
	}
	if !infoSchema.Optional || !infoSchema.ForceNew {
		t.Fatalf("info must be Optional and ForceNew (Optional=%v ForceNew=%v)", infoSchema.Optional, infoSchema.ForceNew)
	}
	if infoSchema.Description == "" {
		t.Fatal("info must have a non-empty Description")
	}
	if infoSchema.DiffSuppressFunc == nil {
		t.Fatal("info must have DiffSuppressFunc set")
	}

	const sampleKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample"

	t.Run("change_info_requires_new", func(t *testing.T) {
		state := &terraform.InstanceState{
			ID: "*1",
			Attributes: map[string]string{
				"id":   "*1",
				"user": "admin",
				"key":  sampleKey,
				"info": "old-label",
			},
		}
		cfg := terraform.NewResourceConfigRaw(map[string]interface{}{
			"user": "admin",
			"key":  sampleKey,
			"info": "new-label",
		})
		cfg.CtyValue = resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
			"user": cty.StringVal("admin"),
			"key":  cty.StringVal(sampleKey),
			"info": cty.StringVal("new-label"),
		}).GetRawConfig()

		diff, err := res.Diff(context.Background(), state, cfg, nil)
		if err != nil {
			t.Fatalf("diff failed: %v", err)
		}
		if diff == nil || diff.Attributes["info"] == nil || !diff.Attributes["info"].RequiresNew {
			t.Fatalf("expected RequiresNew diff on info, got %#v", diff)
		}
	})

	t.Run("add_info_requires_new", func(t *testing.T) {
		state := &terraform.InstanceState{
			ID: "*1",
			Attributes: map[string]string{
				"id":   "*1",
				"user": "admin",
				"key":  sampleKey,
			},
		}
		cfg := terraform.NewResourceConfigRaw(map[string]interface{}{
			"user": "admin",
			"key":  sampleKey,
			"info": "laptop",
		})
		cfg.CtyValue = resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
			"user": cty.StringVal("admin"),
			"key":  cty.StringVal(sampleKey),
			"info": cty.StringVal("laptop"),
		}).GetRawConfig()

		diff, err := res.Diff(context.Background(), state, cfg, nil)
		if err != nil {
			t.Fatalf("diff failed: %v", err)
		}
		if diff == nil || diff.Attributes["info"] == nil || !diff.Attributes["info"].RequiresNew {
			t.Fatalf("expected RequiresNew diff when adding info, got %#v", diff)
		}
	})

	t.Run("omitted_info_suppressed", func(t *testing.T) {
		state := &terraform.InstanceState{
			ID: "*1",
			Attributes: map[string]string{
				"id":   "*1",
				"user": "admin",
				"key":  sampleKey,
				"info": "from-key-comment",
			},
		}
		cfg := terraform.NewResourceConfigRaw(map[string]interface{}{
			"user": "admin",
			"key":  sampleKey,
		})
		cfg.CtyValue = resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
			"user": cty.StringVal("admin"),
			"key":  cty.StringVal(sampleKey),
		}).GetRawConfig()

		diff, err := res.Diff(context.Background(), state, cfg, nil)
		if err != nil {
			t.Fatalf("diff failed: %v", err)
		}
		if diff != nil && !diff.Empty() {
			t.Fatalf("expected empty diff when info is omitted from config, got %#v", diff.Attributes)
		}
	})

	t.Run("serialize_and_deserialize_info", func(t *testing.T) {
		d := resourceDataWithRawConfig(res, "*1", map[string]cty.Value{
			"user": cty.StringVal("admin"),
			"key":  cty.StringVal(sampleKey),
			"info": cty.StringVal("laptop"),
		})
		item, _ := TerraformResourceDataToMikrotik(res.Schema, d)
		if item["info"] != "laptop" {
			t.Fatalf("expected info=laptop, got %#v", item)
		}
		read := res.TestResourceData()
		if diags := MikrotikResourceDataToTerraform(MikrotikItem{"info": "laptop"}, res.Schema, read); diags.HasError() {
			t.Fatalf("read diagnostics: %v", diags)
		}
		if got := read.Get("info"); got != "laptop" {
			t.Fatalf("read info = %#v, want laptop", got)
		}
	})
}
