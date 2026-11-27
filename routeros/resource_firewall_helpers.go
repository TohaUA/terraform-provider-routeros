package routeros

import (
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// firewallOptionalStringUnsetFields builds MetaSetUnsetFields for every
// optional non-computed TypeString attribute on an IPv4/IPv6 firewall rule
// except free-text and positional fields (comment, log_prefix, place_before).
// Removing any of these matchers/action parameters then emits !<field>=""
// (RouterOS unset) instead of <field>="", which RouterOS rejects for matchers
// such as limit.
func firewallOptionalStringUnsetFields(s map[string]*schema.Schema) *schema.Schema {
	var fields []string
	for name, prop := range s {
		if strings.HasPrefix(name, "___") || !prop.Optional || prop.Computed || prop.Type != schema.TypeString {
			continue
		}
		switch name {
		case KeyComment, "log_prefix", KeyPlaceBefore:
			continue
		}
		fields = append(fields, name)
	}
	sort.Strings(fields)
	return PropSetUnsetFields(fields...)
}
