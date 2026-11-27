terraform {
  required_version = ">= 1.11.0"
  required_providers {
    routeros = {
      source = "terraform-routeros/routeros"
    }
  }
}

# Stage 1 runs over local layer-2 before the router has an IP address.
provider "routeros" {}

variable "l2_interface" {
  type        = string
  description = "Local host interface connected directly to the isolated provisioning VLAN or bench."
}

variable "bootstrap_password" {
  type        = string
  sensitive   = true
  ephemeral   = true
  default     = ""
  description = "Initial RouterOS password (empty string on factory-default RouterOS)."
}

data "routeros_mndp_neighbors" "bench" {
  interface = var.l2_interface
  timeout   = "5s"
}

resource "routeros_mac_bootstrap" "router" {
  interface           = var.l2_interface
  mac_address         = data.routeros_mndp_neighbors.bench.neighbors[0].mac_address
  address             = "192.168.88.1/24"
  target_interface    = "ether2"
  username            = "admin"
  password_wo         = var.bootstrap_password
  password_wo_version = 1

  # Required acknowledgement: post-login MAC-Telnet traffic is cleartext.
  allow_insecure_l2 = true
}

output "router_address" {
  value = routeros_mac_bootstrap.router.router_address
}

output "tls_fingerprint" {
  value = routeros_mac_bootstrap.router.tls_fingerprint
}
