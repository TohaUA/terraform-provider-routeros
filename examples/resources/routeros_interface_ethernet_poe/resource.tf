# RouterOS 7.25.1+. Check the menu's package and hardware requirements.
# Manages an existing entry. Destroy only forgets Terraform state.
resource "routeros_interface_ethernet_poe" "example" {
  name         = "ether5"
  poe_out      = "auto-on"
  poe_priority = 10
}
