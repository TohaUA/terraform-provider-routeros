# Device-specific settings; use only on hardware supporting these controls.
resource "routeros_interface_wifi_radio_settings" "settings" {
  external_antenna = false
  wifi_band        = "5GHz"
}
