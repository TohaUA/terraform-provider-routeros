# RouterOS 7.25+. Check the menu's package and hardware requirements.
resource "routeros_routing_pimsm_interface_template" "example" {
  instance   = routeros_routing_pimsm_instance.example.name
  interfaces = ["ether4"]
  disabled   = true
}

resource "routeros_routing_pimsm_instance" "example" {
  name     = "multicast-v6"
  afi      = "ipv6"
  disabled = true
}
