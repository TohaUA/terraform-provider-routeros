data "routeros_routing_bgp_session" "established" {
  filter = {
    established = "true"
  }
}
