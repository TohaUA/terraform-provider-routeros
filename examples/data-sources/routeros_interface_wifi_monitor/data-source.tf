data "routeros_interface_wifi_monitor" "wifi1" {
  arguments = {
    numbers = "wifi1"
  }
}

# Reads one snapshot. Driver-dependent outputs include mld_link_id,
# channel (including puncturing markers), and afc_status.
