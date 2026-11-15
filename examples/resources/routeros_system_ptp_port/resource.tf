# RouterOS 7.25+. Check the menu's package and hardware requirements.
resource "routeros_system_ptp_port" "example" {
  ptp       = routeros_system_ptp.example.name
  interface = "ether4"
  role      = "slave"
  vlan_id   = 100
  disabled  = true
}

resource "routeros_system_ptp" "example" {
  name     = "site-clock"
  disabled = true
}
