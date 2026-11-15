# RouterOS 7.25+. Check the menu's package and hardware requirements.
resource "routeros_routing_pimsm_igmp_interface_template" "example" {
  instance                   = routeros_routing_pimsm_instance.example.name
  interfaces                 = ["ether4"]
  robustness                 = 3
  query_interval             = "2m"
  query_response_interval    = "10s"
  last_member_query_interval = "1s"
  disabled                   = true
}

resource "routeros_routing_pimsm_instance" "example" {
  name     = "multicast-v6"
  afi      = "ipv6"
  disabled = true
}
