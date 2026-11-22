resource "routeros_interface_vlan" "interface_vlan" {
  interface = "bridge"
  name      = "VLAN_TEST"
  vlan_id   = 50

  # RouterOS 7.25.1+
  forced_mac_address = "02:00:00:00:07:25"
}
