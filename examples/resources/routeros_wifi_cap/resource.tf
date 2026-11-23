resource "routeros_wifi_cap" "settings" {
  enabled              = true
  discovery_interfaces = ["bridge1"]
}

# Optional CAP-side MLD controls:
# mld_static = true
# mld_datapath = "mld-datapath-profile"
