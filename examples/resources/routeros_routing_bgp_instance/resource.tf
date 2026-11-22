resource "routeros_routing_bgp_instance" "test" {
  name = "bgp-instance"
  as   = "65000"

  # RouterOS 7.25.1+
  always_compare_med = true
}
