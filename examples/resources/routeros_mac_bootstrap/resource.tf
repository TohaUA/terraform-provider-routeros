provider "routeros" {}

variable "bootstrap_password" {
  type      = string
  sensitive = true
  ephemeral = true
  default   = ""
}

resource "routeros_mac_bootstrap" "initial" {
  interface           = "eth0"
  mac_address         = "54:05:AB:01:02:03"
  address             = "192.168.88.1/24"
  target_interface    = "ether2"
  username            = "admin"
  password_wo         = var.bootstrap_password
  password_wo_version = 1
  allow_insecure_l2   = true
}
