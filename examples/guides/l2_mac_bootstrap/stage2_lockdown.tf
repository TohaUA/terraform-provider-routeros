terraform {
  required_version = ">= 1.11.0"
  required_providers {
    routeros = {
      source = "terraform-routeros/routeros"
    }
  }
}

variable "router_address" {
  type        = string
  description = "Bootstrapped IPv4 address from Stage 1."
}

variable "tls_fingerprint" {
  type        = string
  description = "SHA-256 TLS certificate fingerprint from Stage 1."
}

variable "admin_password" {
  type        = string
  sensitive   = true
  ephemeral   = true
  description = "New rotated administrator password."
}

# Stage 2 connects exclusively over HTTPS REST pinned to the Stage 1 TLS leaf certificate.
provider "routeros" {
  hosturl         = "https://${var.router_address}"
  username        = "admin"
  password        = var.admin_password
  tls_fingerprint = var.tls_fingerprint
}

# Disable MAC-Telnet, MAC-WinBox, MAC-Ping, and MNDP neighbor discovery once HTTPS is pinned.
resource "routeros_tool_mac_server" "lockdown" {
  allowed_interface_list = "none"
}

resource "routeros_tool_mac_server_winbox" "lockdown" {
  allowed_interface_list = "none"
}

resource "routeros_tool_mac_server_ping" "lockdown" {
  enabled = false
}

resource "routeros_ip_neighbor_discovery_settings" "lockdown" {
  discover_interface_list = "none"
}
