data "routeros_interface_wifi_radios" "radios" {}

# Each data row includes radio_mac, hw_caps, ml_group, and supported bands.
# Radios sharing an ml_group can participate in MLO when hardware permits it.
