# RouterOS 7.25+. Check the menu's package and hardware requirements.
resource "routeros_interface_eoipv6" "example" {
  name           = "eoipv6-site"
  remote_address = "2001:db8::2"
  local_address  = "2001:db8::1"
  tunnel_id      = 725
  arp_accept     = false
  disabled       = true
}
