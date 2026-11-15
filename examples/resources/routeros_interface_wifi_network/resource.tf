# RouterOS 7.25+. Check the menu's package and hardware requirements.
variable "wifi_passphrase" {
  type      = string
  sensitive = true
}

resource "routeros_interface_wifi_network" "example" {
  ssid                          = "Example"
  mode                          = "ap"
  security_authentication_types = ["wpa2-psk"]
  security_passphrase           = var.wifi_passphrase
  interworking_domain_names     = ["example.com"]
  interworking_internet         = true
  steering_rrm                  = true
  aaa_nas_identifier            = "site-ap"
}
