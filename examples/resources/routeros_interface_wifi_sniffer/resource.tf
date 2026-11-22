# RouterOS 7.25.1+. Check the menu's package and hardware requirements.
resource "routeros_interface_wifi_sniffer" "example" {
  stream_address = "192.0.2.10"
  stream_port    = 37008
}
