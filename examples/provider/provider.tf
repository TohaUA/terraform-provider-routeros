terraform {
  required_providers {
    routeros = {
      source = "terraform-routeros/routeros"
    }
  }
}

provider "routeros" {
  hosturl        = "https://router.example"      # env ROS_HOSTURL
  username       = "terraform"                   # env ROS_USERNAME
  ca_certificate = "/path/to/ca/certificate.pem" # env ROS_CA_CERTIFICATE
  # Set ROS_PASSWORD in the environment instead of a literal password.
  # insecure (ROS_INSECURE) skips TLS verification (lab use only) and cannot be combined with ca_certificate.
}

resource "routeros_interface_gre" "gre_hq" {
  name           = "gre-hq-1"
  remote_address = "10.77.3.26"
  disabled       = true
}
