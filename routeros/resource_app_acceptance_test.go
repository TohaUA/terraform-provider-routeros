package routeros

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccApp7251(t *testing.T) {
	testAccPreCheck(t)
	if os.Getenv("ROS_APP_TESTS") != "1" {
		t.Skip("set ROS_APP_TESTS=1 on a disposable router with container package and container device mode enabled")
	}
	for _, transport := range testNames {
		t.Run(transport, func(t *testing.T) {
			config := func(description string, privileged bool) string {
				return providerConfig + fmt.Sprintf(`
resource "routeros_app_settings" "test" {
  auto_update = false
  show_in_webfig = false
  lan_bridge = "bridge"
}

resource "routeros_app" "test" {
  network = "lan"
  use_https = false
  yaml = <<-YAML
name: tf-acc-app7251
descr: %s
services:
  web:
    image: docker.io/library/alpine:3.22
    privileged: %t
    environment:
      ROUTER: "[networkRouterIP]"
      TOKEN: acceptance-secret
YAML
}
data "routeros_apps" "test" {
  filter = { name = routeros_app.test.name }
}
`, description, privileged)
			}
			resource.Test(t, resource.TestCase{
				PreCheck: func() { testSetTransportEnv(t, transport) }, ProviderFactories: testAccProviderFactories,
				CheckDestroy: testCheckResourceDestroy("/app", "routeros_app"),
				Steps: []resource.TestStep{
					{Config: config("created", true), Check: resource.ComposeTestCheckFunc(
						resource.TestCheckResourceAttr("routeros_app.test", "name", "tf-acc-app7251"),
						resource.TestCheckResourceAttr("routeros_app.test", "disabled", "true"),
						resource.TestCheckResourceAttr("routeros_app.test", "custom", "true"),
						resource.TestCheckResourceAttr("data.routeros_apps.test", "data.#", "1"),
					)},
					{Config: config("updated", false), Check: resource.TestCheckResourceAttr("routeros_app.test", "description", "updated")},
					{Config: config("updated", false), PlanOnly: true, ExpectNonEmptyPlan: false},
					{ResourceName: "routeros_app.test", ImportState: true, ImportStateVerify: true},
					{ResourceName: "routeros_app_settings.test", ImportState: true, ImportStateVerify: true},
				},
			})
		})
	}
}

func TestAccApp7251Deployment(t *testing.T) {
	testAccPreCheck(t)
	if os.Getenv("ROS_APP_RUN_TESTS") != "1" {
		t.Skip("set ROS_APP_RUN_TESTS=1 with container mode and App storage configured; downloads a container image")
	}
	for _, transport := range testNames {
		t.Run(transport, func(t *testing.T) {
			base := func(disabled bool) string {
				return providerConfig + fmt.Sprintf(`
resource "routeros_app_settings" "test" {
  router_ip = "192.0.2.1"
}
resource "routeros_app" "test" {
  network = "internal"
  disabled = %t
  use_https = false
  yaml = <<-YAML
name: tf-acc-app-running7251
services:
  worker:
    image: docker.io/library/alpine:3.22
    privileged: true
    command: ["/bin/sh", "-c", "while true; do sleep 60; done"]
    environment:
      ROUTER: "[networkRouterIP]"
YAML
}
`, disabled)
			}
			waitRunning := func(_ *terraform.State) error {
				deadline := time.Now().Add(5 * time.Minute)
				for time.Now().Before(deadline) {
					rows, err := ReadItems(&ItemId{Name, "tf-acc-app-running7251"}, "/app", testAccProvider.Meta().(Client))
					if err != nil {
						return err
					}
					if len(*rows) == 1 && (*rows)[0]["running"] == "true" {
						return nil
					}
					time.Sleep(2 * time.Second)
				}
				return fmt.Errorf("App did not reach running state within five minutes")
			}
			action := func(name string) string {
				return fmt.Sprintf(`
resource "routeros_app_%s" "%s" {
  numbers = routeros_app.test.id
}
`, name, name)
			}
			resource.Test(t, resource.TestCase{
				PreCheck: func() { testSetTransportEnv(t, transport) }, ProviderFactories: testAccProviderFactories,
				CheckDestroy: testCheckResourceDestroy("/app", "routeros_app"),
				Steps: []resource.TestStep{
					{Config: base(false), Check: waitRunning},
					{Config: base(false) + action("restart"), Check: waitRunning},
					{Config: base(false) + action("restart") + action("update"), Check: waitRunning},
					{Config: base(true)},
					{Config: base(true) + action("cleanup")},
				},
			})
		})
	}
}
