resource "routeros_interface_dot1x_server" "ether2" {
  auth_types = ["mac-auth"]
  interface  = "ether2"

  # Translated to reauth-period on RouterOS 7.25.1+.
  reauth_timeout = "5m"
}
