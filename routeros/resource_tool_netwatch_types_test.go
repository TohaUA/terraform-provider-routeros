package routeros

import "testing"

func TestNetwatchProbeTypes(t *testing.T) {
	validate := ResourceToolNetwatch().Schema["type"].ValidateFunc
	if validate == nil {
		t.Fatal("Netwatch type has no validator")
	}
	for _, probeType := range []string{"icmp", "tcp-conn", "http-get", "https-get", "dns", "simple"} {
		t.Run(probeType, func(t *testing.T) {
			if _, errs := validate(probeType, "type"); len(errs) != 0 {
				t.Errorf("supported probe type %q rejected: %v", probeType, errs)
			}
		})
	}
	for _, probeType := range []string{"ftp", "https", "DNS"} {
		t.Run("invalid_"+probeType, func(t *testing.T) {
			if _, errs := validate(probeType, "type"); len(errs) == 0 {
				t.Errorf("unsupported probe type %q accepted", probeType)
			}
		})
	}
}
