# RouterOS 7.25.1+. Check the menu's package and hardware requirements.
resource "routeros_routing_pimsm_static_rp" "example" {
  instance = routeros_routing_pimsm_instance.example.name
  group    = "ff3e::/16"
  address  = "2001:db8::1"
  disabled = true
}

resource "routeros_routing_pimsm_instance" "example" {
  name     = "multicast-v6"
  afi      = "ipv6"
  disabled = true
}
