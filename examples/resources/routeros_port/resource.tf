# RouterOS 7.25.1+. Check the menu's package and hardware requirements.
# Manages an existing entry. Destroy only forgets Terraform state.
resource "routeros_port" "example" {
  name         = "serial0"
  baud_rate    = "115200"
  flow_control = "none"
}
