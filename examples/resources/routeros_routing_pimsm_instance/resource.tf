# RouterOS 7.25.1+. Check the menu's package and hardware requirements.
resource "routeros_routing_pimsm_instance" "example" {
  name     = "multicast-v6"
  vrf      = "main"
  afi      = "ipv6"
  disabled = true
}
