# RouterOS 7.25+. Check the menu's package and hardware requirements.
resource "routeros_interface_lte_settings" "example" {
  mode                = "auto"
  esim_channel        = "auto"
  link_recovery_timer = 120
}
