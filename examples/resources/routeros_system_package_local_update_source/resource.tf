resource "routeros_system_package_local_update_source" "hub" {
  address  = "192.168.88.1"
  user     = "package-source"
  password = var.package_source_password
}
