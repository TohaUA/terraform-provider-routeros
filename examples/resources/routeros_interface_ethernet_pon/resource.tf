# RouterOS 7.25+. Check the menu's package and hardware requirements.
# Manages an existing entry. Destroy only forgets Terraform state.
resource "routeros_interface_ethernet_pon" "example" {
  name        = "pon1"
  olt_max_mtu = 1500
}
