provider "routeros" {}

data "routeros_mndp_neighbors" "lab" {
  interface = "eth0"
  timeout   = "5s"
}

output "discovered_neighbors" {
  value = data.routeros_mndp_neighbors.lab.neighbors
}
