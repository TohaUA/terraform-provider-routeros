# RouterOS 7.25.1+. Check the menu's package and hardware requirements.
resource "routeros_ipv6_dhcp_server_binding" "example" {
  address  = "2001:db8::10/128"
  duid     = "0x00030001aabbccddee01"
  iaid     = 725
  server   = "all"
  disabled = true
}
