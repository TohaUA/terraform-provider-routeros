terraform {
  required_providers {
    routeros = {
      source = "terraform-routeros/routeros"
    }
  }
}

variable "monitoring_address" {
  type = string
}

resource "routeros_ip_firewall_addr_list" "monitoring" {
  address = var.monitoring_address
  list    = "monitoring"
}
