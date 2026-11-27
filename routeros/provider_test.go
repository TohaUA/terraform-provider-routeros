package routeros

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

var testAccProvider *schema.Provider
var testAccProviderFactories map[string]func() (*schema.Provider, error)
var testNames = []string{"API", "REST"}

var reHost = regexp.MustCompile(`^(?:\S+://)?(\S+?)(?::\d+)*$`)
var reVersion = regexp.MustCompile(`\d+`)

var providerConfig = `
provider "routeros" {
	insecure = true
}
`

func init() {
	testAccProvider = Provider()
	testAccProviderFactories = map[string]func() (*schema.Provider, error){
		"routeros": func() (*schema.Provider, error) {
			return testAccProvider, nil
		},
	}
}

func testSetTransportEnv(t *testing.T, testName string) {
	host := reHost.FindStringSubmatch(os.Getenv("ROS_HOSTURL"))
	switch {
	case strings.Contains(testName, "API"):
		if err := os.Setenv("ROS_HOSTURL", "apis://"+host[1]); err != nil {
			t.Error(err)
		}
	case strings.Contains(testName, "REST"):
		if err := os.Setenv("ROS_HOSTURL", "https://"+host[1]); err != nil {
			t.Error(err)
		}
	default:
		t.Fatal("Unsupported test name format. The test must have the suffix API or REST.")
	}

}

func testAccPreCheck(t *testing.T) {
	// resource.Test only runs a PreCheck under TF_ACC. A test that calls this
	// directly, such as TestClientTransport_SendRequest, needs the same rule, or a
	// plain `go test` fails it for lacking a router it was never meant to reach.
	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance test: set TF_ACC, ROS_HOSTURL and ROS_USERNAME to run it")
	}

	if os.Getenv("ROS_HOSTURL") == "" ||
		os.Getenv("ROS_USERNAME") == "" {
		t.Fatal("Environment variables (ROS_HOSTURL & ROS_USERNAME) must be set for testing")
	}

	for _, v := range Provider().ResourcesMap {
		checkResourceSchema(v.Schema, t)
	}
}

func checkResourceSchema(s map[string]*schema.Schema, t *testing.T) {
	f, ok := s[MetaResourcePath]
	if !ok {
		t.Fatalf("the schema does not contain field '%v'", MetaResourcePath)
	}
	if f.Default.(string) == "" {
		t.Fatalf("the field '%v', contains no data", MetaResourcePath)
	}
	f, ok = s[MetaId]
	if !ok {
		t.Fatalf("the schema does not contain field '%v'", MetaId)
	}
	if f.Default.(int) < 1 {
		t.Fatalf("the field '%v' is not defined", MetaId)
	}
}

func testCheckResourceDestroy(resourcePath, resourceType string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		cApi, _ := testAccProvider.Meta().(*ApiClient)
		cRest, _ := testAccProvider.Meta().(*RestClient)
		var testTransport TransportType

		switch testAccProvider.Meta().(type) {
		case *ApiClient:
			testTransport = TransportAPI
		case *RestClient:
			testTransport = TransportREST
		default:
			panic("[testCheckResourceDestroy] wrong transport type")
		}

		for _, rs := range s.RootModule().Resources {
			if rs.Type != resourceType {
				continue
			}
			id := rs.Primary.ID
			idName := IdType(Provider().ResourcesMap[resourceType].Schema[MetaId].Default.(int)).String()

			switch testTransport {
			case TransportAPI:
				cmd := []string{resourcePath + "/print", "?" + idName + "=" + id}
				session, err := cApi.pool.acquire()
				if err != nil {
					return nil
				}
				res, err := session.run(cmd, cApi.pool.timeout)
				cApi.pool.release(session, sessionUsable(err))
				if err != nil {
					return nil
				}

				if len(res.Re) > 0 {
					return fmt.Errorf("resource %v %s has been found", resourceType, id)
				}
			case TransportREST:
				req, err := http.NewRequest("GET",
					cRest.HostURL+"/rest"+(&URL{Path: resourcePath, Query: []string{idName + "=" + id}}).GetRestURL(), nil)
				if err != nil {
					return err
				}
				req.Header.Set("Content-Type", "application/json")
				req.SetBasicAuth(cRest.Username, cRest.Password)

				res, err := cRest.Do(req)

				if err != nil {
					return err
				}

				if res == nil {
					return fmt.Errorf("the response body is empty")
				}

				if buf, _ := io.ReadAll(res.Body); string(buf) != "[]" {
					return fmt.Errorf("resource %v %s has been found", resourceType, id)
				}
			}

			return nil
		}

		return nil
	}
}

// testCheckResourceExists queries the MikroTik API and retrieves the matching resource parameters
func testCheckResourceExists(name string, resourcePath string, resource *MikrotikItem) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("%s: resource not found in terraform state", name)
		}

		id := rs.Primary.ID
		if id == "" {
			return fmt.Errorf("%s: no id is set", name)
		}

		resourceType := strings.Split(name, ".")[0]
		resourceSchema, ok := testAccProvider.ResourcesMap[resourceType]
		if !ok {
			return fmt.Errorf("%s: schema for '%s' resource not found", name, resourceType)
		}
		idType := IdType(resourceSchema.Schema[MetaId].Default.(int))

		client := testAccProvider.Meta().(Client)
		resources, err := ReadItems(&ItemId{Type: idType, Value: id}, resourcePath, client)
		if err != nil {
			return err
		}

		if len(*resources) == 0 {
			return fmt.Errorf("%s: resource not found", name)
		}

		if resource != nil {
			*resource = (*resources)[0]
		}

		return nil
	}
}
