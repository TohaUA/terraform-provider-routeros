# Use /ip/firewall/address-list/print show-ids to find the static entry's ID.
terraform import routeros_ip_firewall_addr_list.example_list '*A'
# Alternatively, use an address that occurs only once in the menu.
terraform import routeros_ip_firewall_addr_list.example_list 'address=1.1.1.1'
