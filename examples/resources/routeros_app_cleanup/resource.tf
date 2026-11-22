# Define routeros_app.worker as in the routeros_app example.
# Cleanup permanently removes downloaded images and App data.
# Disable the App before requesting cleanup.
resource "routeros_app_cleanup" "worker" {
  numbers = routeros_app.worker.id
  triggers = {
    request = "cleanup-1" # Change only when another cleanup is intended.
  }
}
