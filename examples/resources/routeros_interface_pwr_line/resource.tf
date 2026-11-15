# RouterOS 7.25+. Check the menu's package and hardware requirements.
# Manages an existing entry. Destroy only forgets Terraform state.
resource "routeros_interface_pwr_line" "example" {
  name       = "pwr-line1"
  arp_accept = false
}
