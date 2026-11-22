resource "routeros_ipv6_pool" "pool-0" {
  name          = "test-pool-0"
  prefix        = "2001:db8:40::/48"
  prefix_length = 64
}

resource "routeros_ipv6_dhcp_server" "test" {
  address_pool = routeros_ipv6_pool.pool-0.name
  interface    = "bridge"
  lease_time   = "1m"
  name         = "test-dhcpv6"
  preference   = 128

  # RouterOS 7.25.1+: publish client address bindings in DNS.
  add_dns_entries        = true
  add_dns_entries_suffix = "example.test"
}
