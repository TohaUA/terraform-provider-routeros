# RouterOS 7.25.1+. Check the menu's package and hardware requirements.
# Executes on apply; a changed trigger runs it again. Destroy cannot undo it.
resource "routeros_ip_cloud_back_to_home_file_settings_remove_certificate" "example" {
  triggers = {
    request = "remove-existing-certificate-1"
  }
}
