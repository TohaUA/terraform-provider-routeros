package routeros

import (
	"os"
	"testing"
)

// unitTestRouterOSVersion is the RouterOS version the unit tests serialize resources
// for when nothing else names one. The serializer stops the process without a version
// rather than guess which attribute renames apply, and the provider only sets one when
// it is configured, which a plain `go test ./routeros/` never does.
const unitTestRouterOSVersion = "7.24"

// TestMain supplies the version up front instead of leaving it to whichever test
// happens to read ROS_VERSION first. ROS_VERSION wins when it is set. Without it, only
// a run that is not an acceptance run (no TF_ACC) gets the default: an acceptance run
// has to say which version it is testing, and one that forgets still fails.
func TestMain(m *testing.M) {
	if RouterOSVersion == "" {
		RouterOSVersion = os.Getenv("ROS_VERSION")
	}
	if RouterOSVersion == "" && os.Getenv("TF_ACC") == "" {
		RouterOSVersion = unitTestRouterOSVersion
	}

	os.Exit(m.Run())
}
