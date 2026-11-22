package routeros

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testInterfaceVethAddress = "routeros_interface_veth.test"

func TestAccInterfaceVethTest_basic(t *testing.T) {
	testAccPreCheck(t)
	host := reHost.FindStringSubmatch(os.Getenv("ROS_HOSTURL"))[1]
	client, err := newApiClient(context.Background(), host+":8728", os.Getenv("ROS_USERNAME"), os.Getenv("ROS_PASSWORD"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if session, err := client.pool.acquire(); err == nil {
			client.pool.release(session, false)
		}
	})
	var interfaces []MikrotikItem
	if err := client.SendRequest(crudRead, &URL{Path: "/interface/veth"}, nil, &interfaces); err != nil {
		if strings.Contains(err.Error(), "no such command") {
			t.Skip("VETH/container support is not installed on this RouterOS device")
		}
		t.Fatal(err)
	}

	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/interface/veth", "routeros_interface_veth"),
				Steps: []resource.TestStep{
					{
						Config: testAccInterfaceVethConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testInterfaceVethAddress),
							resource.TestCheckResourceAttr(testInterfaceVethAddress, "name", "veth-test"),
						),
					},
				},
			})
		})
	}
}

func testAccInterfaceVethConfig() string {
	return providerConfig + `

resource "routeros_interface_veth" "test" {
  name    = "veth-test"
  address = ["192.168.120.2/24"]
  gateway = "192.168.120.1"
  comment = "Virtual interface"
}
`
}
