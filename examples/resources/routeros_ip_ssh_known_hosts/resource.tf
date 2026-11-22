# RouterOS 7.25.1+. Check the menu's package and hardware requirements.
variable "ssh_host_public_key" {
  type = string
}

resource "routeros_ip_ssh_known_hosts" "example" {
  host    = "192.0.2.10"
  key     = var.ssh_host_public_key
  comment = "Verified server host key"
}
