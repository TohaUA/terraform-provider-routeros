resource "routeros_interface_wifi_network_radio" "radio" {
  # Replace with a selector matching your compatible 6 GHz radios.
  labels                        = "YOUR_6GHZ_RADIO_SELECTOR"
  extra_labels                  = ["wifi7"]
  configuration_country         = "Latvia"
  configuration_distance        = "0.1" # Kilometers.
  channel_band                  = "6ghz-be"
  channel_width                 = "20/40/80/160/320mhz"
  channel_preamble_puncturing   = "yes"
  channel_afc                   = false
  security_authentication_types = ["wpa3-psk"]
  disabled                      = true
}
