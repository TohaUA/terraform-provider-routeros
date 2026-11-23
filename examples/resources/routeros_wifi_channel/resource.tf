resource "routeros_wifi_channel" "channel1" {
  name                = "1"
  band                = "2ghz-n"
  frequency           = [2412]
  secondary_frequency = ["disabled"]
  skip_dfs_channels   = "disabled"
  width               = "20mhz"
}

# Optional WiFi 7 controls, depending on radio capabilities:
# preamble_puncturing = "yes"
# afc = false
