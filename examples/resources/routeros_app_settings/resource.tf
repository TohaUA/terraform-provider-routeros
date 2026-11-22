resource "routeros_app_settings" "settings" {
  # Select an already formatted, mounted disk; this resource does not format it.
  disk           = "sata1"
  lan_bridge     = "bridge"
  auto_update    = false
  show_in_webfig = true
}
