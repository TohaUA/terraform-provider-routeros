data "routeros_ip_neighbor" "all" {
  filter = {
    interface = "ether2"
  }
}

output "neighbor_identities" {
  value = [
    for n in data.routeros_ip_neighbor.all.neighbors : {
      interface      = n.interface
      identity       = n.identity
      interface_name = n.interface_name
      mac_address    = n.mac_address
      discovered_by  = n.discovered_by
    }
  ]
}
