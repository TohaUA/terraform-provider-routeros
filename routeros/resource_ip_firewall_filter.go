package routeros

import (
	"context"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

// ResourceIPFirewallFilter https://wiki.mikrotik.com/wiki/Manual:IP/Firewall/Filter
func ResourceIPFirewallFilter() *schema.Resource {
	resSchema := map[string]*schema.Schema{
		MetaResourcePath: PropResourcePath("/ip/firewall/filter"),
		MetaId:           PropId(Id),
		MetaSkipFields:   PropSkipFields("bytes", "packets"),

		"action": {
			Type:        schema.TypeString,
			Required:    true,
			Description: "Action to take if a packet is matched by the rule",
			ValidateFunc: validation.StringInSlice([]string{
				"accept", "add-dst-to-address-list", "add-src-to-address-list", "drop", "fasttrack-connection",
				"jump", "log", "passthrough", "reject", "return", "tarpit",
			}, false),
		},
		"address_list": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Name of the address list used in 'add-dst-to-address-list' and 'add-src-to-address-list' actions.",
		},
		"address_list_timeout": {
			Type:     schema.TypeString,
			Optional: true,
			Description: "Time interval after which the address will be removed from the address list specified by " +
				"address-list parameter. Used in conjunction with add-dst-to-address-list or add-src-to-address-list " +
				"actions.",
			DiffSuppressFunc: TimeEqual,
		},
		"chain": {
			Type:     schema.TypeString,
			Required: true,
			Description: "Specifies to which chain rule will be added. If the input does not match the name of an " +
				"already defined chain, a new chain will be created.",
		},
		KeyComment: PropCommentRw,
		"connection_bytes": {
			Type:     schema.TypeString,
			Optional: true,
			Description: "Matches packets only if a given amount of bytes has been transfered through the particular " +
				"connection.",
		},
		"connection_limit": {
			Type:     schema.TypeString,
			Optional: true,
			Description: "Matches connections per address or address block after given value is reached. Should be " +
				"used together with connection-state=new and/or with tcp-flags=syn because matcher is very resource " +
				"intensive.",
		},
		"connection_mark": {
			Type:     schema.TypeString,
			Optional: true,
			Description: "Matches packets marked via mangle facility with particular connection mark. If no-mark is " +
				"set, rule will match any unmarked connection.",
		},
		"connection_nat_state": {
			Type:             schema.TypeString,
			Optional:         true,
			Description:      "Can match connections that are srcnatted, dstnatted or both.",
			ValidateDiagFunc: ValidationMultiValInSlice([]string{"srcnat", "dstnat"}, false, true),
		},
		// See comment for the "path_cost" field in resource_interface_bridge_port.go file.
		"connection_rate": {
			Type:     schema.TypeString,
			Optional: true,
			Description: "Connection Rate is a firewall matcher that allow to capture traffic based on present speed " +
				"of the connection (0..4294967295).",
		},
		"connection_state": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Interprets the connection tracking analysis data for a particular packet.",
			ValidateDiagFunc: ValidationMultiValInSlice([]string{
				"established", "invalid", "new", "related", "untracked",
			}, false, true),
		},
		"connection_type": {
			Type:     schema.TypeString,
			Optional: true,
			Description: "Matches packets from related connections based on information from their connection " +
				"tracking helpers.",
			ValidateDiagFunc: ValidationMultiValInSlice([]string{
				"ftp", "h323", "irc", "pptp", "quake3", "sip", "tftp",
			}, false, true),
		},
		"content": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Match packets that contain specified text.",
		},
		KeyDisabled: PropDisabledRw,
		"dscp": {
			Type:         schema.TypeInt,
			Optional:     true,
			Description:  "Matches DSCP IP header field.",
			ValidateFunc: validation.IntBetween(0, 63),
		},
		"dst_address": {
			Type:             schema.TypeString,
			Optional:         true,
			Description:      "Matches packets which destination is equal to specified IP or falls into specified IP range.",
			DiffSuppressFunc: ImplicitSingleHostCIDR4,
		},
		"dst_address_list": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Matches destination address of a packet against user-defined address list.",
		},
		"dst_address_type": {
			Type:             schema.TypeString,
			Optional:         true,
			Description:      "Matches destination address type.",
			ValidateDiagFunc: ValidationMultiValInSlice([]string{"unicast", "local", "broadcast", "multicast"}, false, true),
		},
		"dst_limit": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Matches packets until a given rate is exceeded.",
		},
		"dst_port": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "List of destination port numbers or port number ranges.",
		},
		KeyDynamic: PropDynamicRo,
		"enforce_order": {
			Type:     schema.TypeBool,
			Optional: true,
			Default:  false,
			Description: "Enable opt-in live rule order drift detection and in-place reconciliation against " +
				"`place_before`. When `true`, Terraform verifies on refresh that this rule remains ordered " +
				"before `place_before` and moves it in place if reordered out-of-band or if `place_before` changes.",
		},
		"fragment": {
			Type:     schema.TypeBool,
			Optional: true,
			Description: "Matches fragmented packets. First (starting) fragment does not count. If connection tracking " +
				"is enabled there will be no fragments as system automatically assembles every packet",
		},
		"hotspot": {
			Type:             schema.TypeString,
			Optional:         true,
			Description:      "Matches packets received from HotSpot clients against various HotSpot matchers.",
			ValidateDiagFunc: ValidationMultiValInSlice([]string{"auth", "from-client", "http", "local-dst", "to-client"}, false, true),
		},
		// https://help.mikrotik.com/docs/display/ROS/L3+Hardware+Offloading#L3HardwareOffloading-RoutingFilters
		// Firewall filter rules have hw-offload option for Fasttrack, allowing fine-tuning connection offloading.
		"hw_offload": {
			Type:        schema.TypeBool,
			Optional:    true,
			Description: "Connection offloading for Fasttrack.",
		},
		"icmp_options": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Matches ICMP type: code fields.",
		},
		"in_bridge_port": {
			Type:     schema.TypeString,
			Optional: true,
			Description: "Actual interface the packet has entered the router if the incoming interface is a bridge. " +
				"Works only if use-ip-firewall is enabled in bridge settings.",
		},
		"in_bridge_port_list": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Set of interfaces defined in interface list. Works the same as in-bridge-port.",
		},
		"in_interface": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Interface the packet has entered the router.",
		},
		"in_interface_list": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Set of interfaces defined in interface list. Works the same as in-interface.",
		},
		"ingress_priority": {
			Type:     schema.TypeInt,
			Optional: true,
			Description: "Matches the priority of an ingress packet. Priority may be derived from VLAN, WMM, DSCP, " +
				"or MPLS EXP bit.",
			ValidateFunc: validation.IntBetween(0, 63),
		},
		"invalid": {
			Type:     schema.TypeBool,
			Computed: true,
		},
		"ipsec_policy": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Matches the policy used by IPsec. Value is written in the following format: direction, policy.",
			ValidateFunc: validation.StringMatch(regexp.MustCompile(`^(in|out)\s?,\s?(ipsec|none)$`),
				"Value must be written in the following format: direction, policy."),
		},
		"ipv4_options": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Matches IPv4 header options.",
			ValidateFunc: validation.StringInSlice([]string{
				"any", "loose-source-routing", "no-record-route", "no-router-alert", "no-source-routing",
				"no-timestamp", "none", "record-route", "router-alert", "strict-source-routing", "timestamp",
			}, false),
		},
		"jump_target": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Name of the target chain to jump to. Applicable only if action=jump.",
		},
		"layer7_protocol": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Layer7 filter name.",
		},
		"limit": {
			Type:     schema.TypeString,
			Optional: true,
			Description: "Matches packets up to a limited rate (packet rate or bit rate). A rule using this matcher " +
				"will match until this limit is reached. Parameters are written in the following format: " +
				"rate[/time],burst:mode.",
		},
		"log": {
			Type:        schema.TypeBool,
			Optional:    true,
			Default:     false,
			Description: "Add a message to the system log.",
		},
		"log_prefix": {
			Type:     schema.TypeString,
			Optional: true,
			Description: "Adds specified text at the beginning of every log message. Applicable if action=log or " +
				"log=yes configured.",
		},
		"nth": {
			Type:     schema.TypeString,
			Optional: true,
			Description: "Matches every nth packet: nth=2,1 rule will match every first packet of 2, hence, 50% of " +
				"all the traffic that is matched by the rule",
		},
		"out_bridge_port": {
			Type:     schema.TypeString,
			Optional: true,
			Description: "Actual interface the packet is leaving the router if the outgoing interface is a bridge. " +
				"Works only if use-ip-firewall is enabled in bridge settings.",
		},
		"out_bridge_port_list": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Set of interfaces defined in interface list. Works the same as out-bridge-port.",
		},
		"out_interface": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Interface the packet is leaving the router.",
		},
		"out_interface_list": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Set of interfaces defined in interface list. Works the same as out-interface.",
		},
		"packet_mark": {
			Type:     schema.TypeString,
			Optional: true,
			Description: "Matches packets marked via mangle facility with particular packet mark. If no-mark is set, " +
				"the rule will match any unmarked packet.",
		},
		"packet_size": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Matches packets of specified size or size range in bytes.",
		},
		"per_connection_classifier": {
			Type:     schema.TypeString,
			Optional: true,
			Description: "PCC matcher allows dividing traffic into equal streams with the ability to keep packets " +
				"with a specific set of options in one particular stream.",
		},
		KeyPlaceBefore: {
			Type:     schema.TypeString,
			Optional: true,
			Description: "Anchor rule ID before which this rule must appear. When `enforce_order` is `true`, " +
				"live order drift or anchor changes reconcile in place without recreating the rule.",
		},
		"port": {
			Type:     schema.TypeString,
			Optional: true,
			Description: "Matches if any (source or destination) port matches the specified list of ports or port " +
				"ranges. Applicable only if protocol is TCP or UDP",
		},
		"priority": {
			Type:     schema.TypeInt,
			Optional: true,
			Description: "Matches the packet's priority after a new priority has been set. Priority may be derived from " +
				"VLAN, WMM, DSCP, MPLS EXP bit, or from the priority that has been set using the set-priority action.",
			ValidateFunc: validation.IntBetween(0, 63),
		},
		"protocol": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Matches particular IP protocol specified by protocol name or number.",
		},
		"psd": {
			Type:     schema.TypeString,
			Optional: true,
			Description: "Attempts to detect TCP and UDP scans. Parameters are in the following format WeightThreshold, " +
				"DelayThreshold, LowPortWeight, HighPortWeight.",
		},
		"random": {
			Type:         schema.TypeInt,
			Optional:     true,
			Description:  "Matches packets randomly with a given probability.",
			ValidateFunc: validation.IntBetween(1, 99),
		},
		"reject_with": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Specifies ICMP error to be sent back if the packet is rejected. Applicable if action=reject.",
			ValidateFunc: validation.StringInSlice([]string{
				"icmp-admin-prohibited", "icmp-net-prohibited", "icmp-protocol-unreachable", "icmp-host-prohibited",
				"icmp-network-unreachable", "tcp-reset", "icmp-host-unreachable", "icmp-port-unreachable",
			}, false),
		},
		"routing_table": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Matches packets which destination address is resolved in specific a routing table.",
		},
		"routing_mark": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Matches packets marked by mangle facility with particular routing mark.",
		},
		"src_address": {
			Type:             schema.TypeString,
			Optional:         true,
			Description:      "Matches packets which source is equal to specified IP or falls into a specified IP range.",
			DiffSuppressFunc: ImplicitSingleHostCIDR4,
		},
		"src_address_list": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Matches source address of a packet against user-defined address list.",
		},
		"src_address_type": {
			Type:             schema.TypeString,
			Optional:         true,
			Description:      "Matches source address type.",
			ValidateDiagFunc: ValidationMultiValInSlice([]string{"unicast", "local", "broadcast", "multicast"}, false, true),
		},
		"src_port": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "List of source ports and ranges of source ports. Applicable only if a protocol is TCP or UDP.",
		},
		"src_mac_address": {
			Type:         schema.TypeString,
			Optional:     true,
			Description:  "Matches source MAC address of the packet.",
			ValidateFunc: ValidationMacAddress,
		},
		"tcp_flags": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Matches specified TCP flags.",
		},
		"tcp_mss": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Matches TCP MSS value of an IP packet.",
		},
		"time": {
			Type:     schema.TypeString,
			Optional: true,
			Description: "Allows to create a filter based on the packets' arrival time and date or, for locally " +
				"generated packets, departure time and date.",
		},
		"tls_host": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Allows matching HTTPS traffic based on TLS SNI hostname.",
		},
		"ttl": {
			Type:        schema.TypeString,
			Optional:    true,
			Description: "Matches packets TTL value.",
		},
	}
	resSchema[MetaSetUnsetFields] = firewallOptionalStringUnsetFields(resSchema)
	return &schema.Resource{
		CreateContext: func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			return resourceIPFirewallFilterCreate(ctx, resSchema, d, m)
		},
		ReadContext: func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			return resourceIPFirewallFilterRead(ctx, resSchema, d, m)
		},
		UpdateContext: func(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
			return resourceIPFirewallFilterUpdate(ctx, resSchema, d, m)
		},
		DeleteContext: DefaultDelete(resSchema),
		Importer: &schema.ResourceImporter{
			StateContext: ImportStateCustomContext(resSchema),
		},

		Schema: resSchema,
	}
}

func firewallFilterReadSchema(resSchema map[string]*schema.Schema) map[string]*schema.Schema {
	return schemaWithSkipFields(resSchema, "enforce_order")
}

func findFirewallRuleAndAnchorIndex(rules []MikrotikItem, ruleID, anchorID string) (int, int) {
	ruleIdx, anchorIdx := -1, -1
	for i, r := range rules {
		id := r[".id"]
		if id == ruleID {
			ruleIdx = i
		}
		if id == anchorID {
			anchorIdx = i
		}
	}
	return ruleIdx, anchorIdx
}

func resourceIPFirewallFilterCreate(ctx context.Context, resSchema map[string]*schema.Schema, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	enforceOrder := d.Get("enforce_order").(bool)
	anchor := strings.TrimSpace(d.Get(KeyPlaceBefore).(string))
	if enforceOrder && anchor == "" {
		return diag.Errorf("enforce_order requires place_before to be set")
	}

	diags := ResourceCreate(ctx, schemaWithSkipFields(resSchema, "enforce_order"), d, m)
	if diags.HasError() || d.Id() == "" {
		return diags
	}

	if enforceOrder {
		return resourceIPFirewallFilterRead(ctx, resSchema, d, m)
	}
	return diags
}

func resourceIPFirewallFilterRead(ctx context.Context, resSchema map[string]*schema.Schema, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	diags := ResourceRead(ctx, firewallFilterReadSchema(resSchema), d, m)
	if diags.HasError() || d.Id() == "" {
		return diags
	}

	if !d.Get("enforce_order").(bool) {
		return diags
	}
	anchor := strings.TrimSpace(d.Get(KeyPlaceBefore).(string))
	if anchor == "" {
		return diags
	}

	rules, err := ReadItems(nil, "/ip/firewall/filter", m.(Client))
	if err != nil {
		return append(diags, diag.FromErr(err)...)
	}

	ruleIdx, anchorIdx := findFirewallRuleAndAnchorIndex(*rules, d.Id(), anchor)
	if ruleIdx >= 0 && anchorIdx >= 0 && ruleIdx < anchorIdx {
		return diags
	}

	if err := d.Set(KeyPlaceBefore, ""); err != nil {
		return append(diags, diag.FromErr(err)...)
	}
	return diags
}

func resourceIPFirewallFilterUpdate(ctx context.Context, resSchema map[string]*schema.Schema, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	enforceOrder := d.Get("enforce_order").(bool)
	anchor := strings.TrimSpace(d.Get(KeyPlaceBefore).(string))
	if enforceOrder && anchor == "" {
		return diag.Errorf("enforce_order requires place_before to be set")
	}

	hasOrderChange := d.HasChange(KeyPlaceBefore) || d.HasChange("enforce_order")
	hasNonOrderChange := false
	for k := range resSchema {
		if strings.HasPrefix(k, "___") || k == KeyPlaceBefore || k == "enforce_order" {
			continue
		}
		if d.HasChange(k) {
			hasNonOrderChange = true
			break
		}
	}

	if hasNonOrderChange || !hasOrderChange {
		// place_before and enforce_order must not be sent on property updates.
		// Copy via schemaWithSkipFields so resSchema stays concurrency-safe.
		diags := ResourceUpdate(ctx, schemaWithSkipFields(resSchema, KeyPlaceBefore, "enforce_order"), d, m)
		if diags.HasError() {
			return diags
		}
	}

	if anchor != "" && (d.HasChange(KeyPlaceBefore) || enforceOrder) {
		if anchor == d.Id() {
			return diag.Errorf("rule %q cannot use itself as place_before anchor", d.Id())
		}

		c := m.(Client)
		rules, err := ReadItems(nil, "/ip/firewall/filter", c)
		if err != nil {
			return diag.FromErr(err)
		}

		ruleIdx, anchorIdx := findFirewallRuleAndAnchorIndex(*rules, d.Id(), anchor)
		if anchorIdx == -1 {
			diags := resourceIPFirewallFilterRead(ctx, resSchema, d, m)
			return append(diags, diag.Errorf("anchor rule %q not found in /ip/firewall/filter", anchor)...)
		}
		if ruleIdx == -1 {
			return diag.Errorf("managed rule %q not found in /ip/firewall/filter", d.Id())
		}

		if ruleIdx > anchorIdx {
			movePath := "/ip/firewall/filter"
			if c.GetTransport() == TransportREST {
				movePath += "/move"
			}
			if err := c.SendRequest(crudMove, &URL{Path: movePath}, MikrotikItem{
				"numbers":     d.Id(),
				"destination": anchor,
			}, nil); err != nil {
				diags := resourceIPFirewallFilterRead(ctx, resSchema, d, m)
				return append(diags, diag.Errorf("failed to move rule %q before anchor rule %q: %v", d.Id(), anchor, err)...)
			}

			rules, err = ReadItems(nil, "/ip/firewall/filter", c)
			if err != nil {
				diags := resourceIPFirewallFilterRead(ctx, resSchema, d, m)
				return append(diags, diag.FromErr(err)...)
			}
			ruleIdx, anchorIdx = findFirewallRuleAndAnchorIndex(*rules, d.Id(), anchor)
			if ruleIdx < 0 || anchorIdx < 0 || ruleIdx >= anchorIdx {
				diags := resourceIPFirewallFilterRead(ctx, resSchema, d, m)
				return append(diags, diag.Errorf("rule %q was not ordered before anchor rule %q in /ip/firewall/filter after move", d.Id(), anchor)...)
			}
		}
	}

	return resourceIPFirewallFilterRead(ctx, resSchema, d, m)
}
