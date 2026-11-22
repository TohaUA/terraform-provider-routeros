# RouterOS 7.25.1+. Check the menu's package and hardware requirements.
resource "routeros_system_ptp" "example" {
  name      = "site-clock"
  ptp_mode  = "transparent-clock"
  transport = "l2-forwardable"
  disabled  = true
}
