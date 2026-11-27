package routeros

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func TestQueueTypeCakeOverheadSchemeValidation(t *testing.T) {
	f := ResourceQueueType().Schema["cake_overhead_scheme"].ValidateFunc
	for _, v := range []string{"conservative", "ethernet", "pppoe-ptm", "via-ethernet"} {
		if _, errs := f(v, "cake_overhead_scheme"); len(errs) != 0 {
			t.Errorf("%q rejected: %v", v, errs)
		}
	}
	for _, v := range []string{"convervative", "bogus"} {
		if _, errs := f(v, "cake_overhead_scheme"); len(errs) == 0 {
			t.Errorf("%q accepted", v)
		}
	}
}

func TestSystemLoggingTopics725(t *testing.T) {
	have := map[string]bool{}
	for _, v := range validTopics {
		have[v] = true
	}
	for _, v := range []string{"acme-client", "amt", "discover", "evpn", "mvrp", "poe-in", "pon", "ptp",
		"rproxy", "socksify", "ssld", "wiliot", "zerotier"} {
		if !have[v] {
			t.Errorf("topic %q missing", v)
		}
	}
}

func TestSNMPCommunityAuthenticationProtocol725(t *testing.T) {
	f := ResourceSNMPCommunity().Schema["authentication_protocol"].ValidateFunc
	for _, v := range []string{"MD5", "SHA1", "SHA256", "SHA384", "SHA512"} {
		if _, errs := f(v, "authentication_protocol"); len(errs) != 0 {
			t.Errorf("%q rejected: %v", v, errs)
		}
	}
	for _, v := range []string{"SHA224", "bogus"} {
		if _, errs := f(v, "authentication_protocol"); len(errs) == 0 {
			t.Errorf("%q accepted", v)
		}
	}
}

func TestIpIpsecKeySize725(t *testing.T) {
	f := ResourceIpIpsecKey().Schema["key_size"].ValidateFunc
	for _, v := range []int{2048, 4096, 8192} {
		if _, errs := f(v, "key_size"); len(errs) != 0 {
			t.Errorf("%d rejected: %v", v, errs)
		}
	}
	for _, v := range []int{1024, 1536} {
		if _, errs := f(v, "key_size"); len(errs) == 0 {
			t.Errorf("%d accepted", v)
		}
	}
}

func TestRouterOS725RemovedAttributesAndResources(t *testing.T) {
	if _, ok := Provider().ResourcesMap["routeros_bridge_mlag"]; ok {
		t.Error("routeros_bridge_mlag must not be registered")
	}

	sshSchema := ResourceIpSSHServer().Schema
	for _, field := range []string{"allow_none_crypto", "always_allow_password_login"} {
		if _, ok := sshSchema[field]; ok {
			t.Errorf("routeros_ip_ssh_server must not define %q", field)
		}
	}

	if _, ok := ResourceSystemLoggingAction().Schema["bsd_syslog"]; ok {
		t.Error("routeros_system_logging_action must not define bsd_syslog")
	}

	for name, res := range map[string]*schema.Resource{
		"routeros_routing_bgp_connection": ResourceRoutingBgpConnection(),
		"routeros_routing_bgp_template":   ResourceRoutingBgpTemplate(),
	} {
		for _, field := range []string{"add_path_out", "cluster_id", "router_id"} {
			if _, ok := res.Schema[field]; ok {
				t.Errorf("%s must not define %q", name, field)
			}
		}
		inputSchema := res.Schema["input"].Elem.(*schema.Resource).Schema
		for _, field := range []string{"accept_unknown", "ignore_as_path_len"} {
			if _, ok := inputSchema[field]; ok {
				t.Errorf("%s.input must not define %q", name, field)
			}
		}
	}

	vpnImportSchema := ResourceRoutingBgpVpn().Schema["import"].Elem.(*schema.Resource).Schema
	if _, ok := vpnImportSchema["router_id"]; ok {
		t.Error("routeros_routing_bgp_vpn.import must not define router_id")
	}
}
