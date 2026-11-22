# RouterOS 7.25.1+. Check the menu's package and hardware requirements.
resource "routeros_ip_cloud_back_to_home_user" "example" {
  name                   = "mobile-user"
  client_dns             = "192.0.2.53"
  client_allowed_address = "192.0.2.0/24"
  allow_lan              = true
}
