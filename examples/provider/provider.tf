terraform {
  required_providers {
    routeros = {
      source = "terraform-routeros/routeros"
    }
  }
}

provider "routeros" {
  hosturl        = "https://router.local"        # env ROS_HOSTURL or MIKROTIK_HOST
  username       = "admin"                       # env ROS_USERNAME or MIKROTIK_USER
  ca_certificate = "/path/to/ca/certificate.pem" # env ROS_CA_CERTIFICATE or MIKROTIK_CA_CERTIFICATE
  # Set ROS_PASSWORD (or MIKROTIK_PASSWORD) in the environment instead of a literal password.
  # insecure (ROS_INSECURE / MIKROTIK_INSECURE) skips TLS verification (lab use only) and cannot be combined with ca_certificate.
}

resource "routeros_interface_gre" "gre_hq" {
  name           = "gre-hq-1"
  remote_address = "10.77.3.26"
  disabled       = true
}
