package routeros

import (
	"maps"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// RouterOS reports route blackholes as a presence marker ("blackhole": "")
// as well as an explicit boolean on older responses. Normalize only this
// route flag: an empty value on an ordinary boolean still means false.
// Supply false when absent so refresh also notices a blackhole removed
// outside Terraform. The copy keeps the client's response unchanged.
func normalizeRouteBlackholeRead(item MikrotikItem, s map[string]*schema.Schema) MikrotikItem {
	path, ok := s[MetaResourcePath]
	if !ok || (path.Default != "/ip/route" && path.Default != "/ipv6/route") {
		return item
	}
	value, present := item["blackhole"]
	if present && value != "" {
		return item
	}
	normalized := maps.Clone(item)
	if normalized == nil {
		normalized = MikrotikItem{}
	}
	if present {
		normalized["blackhole"] = "true"
	} else {
		normalized["blackhole"] = "false"
	}
	return normalized
}
