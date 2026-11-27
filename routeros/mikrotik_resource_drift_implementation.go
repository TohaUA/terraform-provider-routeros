package routeros

import (
	"fmt"
	"strconv"
	"strings"
)

var driftAttributeSlice = driftTable{}

// Drift objects

type driftTable map[string][]driftAttribute

type driftAttribute struct {
	TF string
	MT string
}

// Add registers an unconditional attribute rename for a RouterOS resource path.
func (dt *driftTable) Add(resourcePath, attrTF, attrMT string) {
	(*dt)[resourcePath] = append((*dt)[resourcePath], driftAttribute{TF: attrTF, MT: attrMT})
}

// GetDriftMap returns a map matching TF attributes and MT parameters for a resource path.
// reverse=false returns TF -> MT, reverse=true returns MT -> TF.
func (dt *driftTable) GetDriftMap(resName string, reverse bool) map[string]string {
	res := map[string]string{}
	for _, attr := range (*dt)[resName] {
		if !reverse {
			res[attr.TF] = attr.MT
		} else {
			res[attr.MT] = attr.TF
		}
	}
	return res
}

// Parse RouterOS version.

func parseRouterOSVersion(ros string) (version uint64, err error) {
	for i, p := range strings.Split(ros, ".") {
		// The version is packed into three bytes, so only major.minor.patch fit.
		// This has to stop before the shift rather than after it: at i == 3 the
		// shift count is (2-3)*8 == -8, and Go panics on a negative shift amount.
		// The bound used to sit below and never ran.
		if i > 2 {
			break
		}

		var u uint64
		if u, err = strconv.ParseUint(p, 10, 64); err != nil {
			err = fmt.Errorf("RouterOS version parts parsing error, %v", err)
			return
		} else {
			version += u << ((2 - i) * 8)
		}
	}

	if version == 0 {
		err = fmt.Errorf("RouterOS version parsing error, version is zero")
	}
	return
}
