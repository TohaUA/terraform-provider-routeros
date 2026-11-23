resource "routeros_wifi_security" "security1" {
  name                 = "security1"
  authentication_types = ["wpa2-psk", "wpa3-psk"]
  ft                   = true
  ft_preserve_vlanid   = true
  passphrase           = "password"
}

# Optional WiFi 7 / fast-roaming controls:
# beacon_protection = "enabled"
# ft_r0_key_lifetime = "1h"
