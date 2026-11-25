package routeros

import "testing"

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
