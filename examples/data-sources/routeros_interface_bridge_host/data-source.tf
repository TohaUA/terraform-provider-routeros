data "routeros_interface_bridge_host" "bridge" {
  filter = {
    bridge = "bridge"
  }
}

output "bridge_mac_table" {
  value = [
    for h in data.routeros_interface_bridge_host.bridge.hosts : {
      bridge      = h.bridge
      interface   = h.interface
      vid         = h.vid
      mac_address = h.mac_address
      dynamic     = h.dynamic
    }
  ]
}
