package routeros

import (
	"cmp"
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"
)

var driftAttributeSlice driftObjects

// Drift objects

type driftObjects []driftObject

type driftObject struct {
	Version   uint64
	ros       string
	Resources map[string][]driftAttribute
}

type driftAttribute struct {
	TF string
	MT string
}

// Add an object to the slice.
// ros - RouterOS version (7.17.2)
// resourcePath - resource path
// attrTF - attribute name in the latest schema version
// attrMT - attribute name in MikroTik parameters
func (do *driftObjects) Add(ros, resourcePath, attrTF, attrMT string) {
	version, err := parseRouterOSVersion(ros)
	if err != nil {
		log.Fatal(err)
	}

	if i := do.index(version); i != -1 {
		(*do)[i].Resources[resourcePath] = append((*do)[i].Resources[resourcePath], driftAttribute{attrTF, attrMT})
	} else {
		*do = append(*do, driftObject{
			Version:   version,
			ros:       ros,
			Resources: map[string][]driftAttribute{resourcePath: {{attrTF, attrMT}}},
		})
	}
}

// Sorting a slice in descending order.
func (do *driftObjects) SortDesc() {
	slices.SortFunc(driftAttributeSlice, func(a, b driftObject) int {
		return cmp.Compare(a.Version, b.Version) * -1
	})
}

// Getting the object index by RouterOS version.
func (do *driftObjects) index(version uint64) int {
	for i := range *do {
		if version == (*do)[i].Version {
			return i
		}
	}
	return -1
}

// Obtaining a map to match TF attributes and MT parameters for further transformation.
// Direct output (for TF to MT serialization) and reverse output (MT to TF) are provided.
func (do *driftObjects) GetDriftMap(ros, resName string, reverse bool) (res map[string]string) {
	// An empty version is as fatal as a malformed one. The provider sets it before
	// any resource is serialized, and a moment without it (another provider
	// configuration in the same process re-detecting its version) must not quietly
	// fall back to no renames: that sends attribute names the router does not use,
	// such as address instead of available-from on 7.24.
	version, err := parseRouterOSVersion(ros)
	if err != nil {
		log.Fatal(err)
	}

	res = map[string]string{}
	for i := range *do {
		if version >= (*do)[i].Version {
			for _, attr := range (*do)[i].Resources[resName] {
				if !reverse {
					res[attr.TF] = attr.MT
				} else {
					res[attr.MT] = attr.TF
				}
			}
		}
	}
	return
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
