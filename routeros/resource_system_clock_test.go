package routeros

import (
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testSystemClockTask = "routeros_system_clock.set"

func TestAccSystemClockTest_basic(t *testing.T) {
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				Steps:             makeSteps(name),
			})

		})
	}
}

func makeSteps(name string) (res []resource.TestStep) {
	// RouterOS refuses "cannot set time before package build time", so a
	// hardcoded date only works until the device under test is built after it.
	// The dates here used to be in 2024 and started failing on 7.24, which was
	// built in August 2026. Deriving them from the clock keeps the test honest
	// about what it is checking -- that a date round-trips -- without pinning it
	// to a moment that expires.
	apiDate := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	restDate := time.Now().AddDate(0, 0, 2).Format("2006-01-02")

	params := map[string]map[string]string{
		"API": {
			"date":           apiDate,
			"time":           `17:58:11`,
			"time_zone_name": `EST`,
		},
		"REST": {
			"date":           restDate,
			"time":           `18:58:11`,
			"time_zone_name": `UTC`,
		},
	}

	for k, v := range params[name] {
		checks := []resource.TestCheckFunc{
			testResourcePrimaryInstanceId(testSystemClockTask),
		}

		// State holds the value applied rather than the router's running clock,
		// so the time of day compares exactly like everything else.
		checks = append(checks, resource.TestCheckResourceAttr(testSystemClockTask, k, v))

		res = append(res, resource.TestStep{
			Config: fmt.Sprintf(`%v
			resource "routeros_system_clock" "set" {
				%v = "%v"
			}`, providerConfig, k, v),
			Check: resource.ComposeTestCheckFunc(checks...),
		})

	}

	// A deliberate change to a time already applied has to reach the router. An
	// earlier version suppressed every difference between two valid times, so
	// the second of these steps planned nothing and the update never ran.
	for _, v := range []string{"10:15:30", "11:45:00"} {
		res = append(res, resource.TestStep{
			Config: fmt.Sprintf(`%v
			resource "routeros_system_clock" "set" {
				time = "%v"
			}`, providerConfig, v),
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr(testSystemClockTask, "time", v),
			),
		})
	}

	return
}
