resource "routeros_ip_dhcp_server" "server" {
  address_pool = "my_address_pool"
  interface    = "bridge"
  name         = "bridge_dhcp"

  # RouterOS 7.25+: offer IPv6-only operation to supporting clients.
  ipv6_only_preferred = true
}
