resource "routeros_ip_arp" "example" {
  address     = "192.0.2.50"
  interface   = "ether2"
  mac_address = "00:11:22:33:44:55"
  comment     = "Static ARP entry"
}
