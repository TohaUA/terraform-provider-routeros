# Import by RouterOS internal ID:
terraform import routeros_ip_arp.example "*1"

# Import by address and interface (recommended when the same IPv4 address exists on multiple interfaces):
terraform import routeros_ip_arp.example "192.0.2.50,ether2"

# Or using explicit attribute filters:
terraform import routeros_ip_arp.example "address=192.0.2.50,interface=ether2"
