# Define routeros_app.worker as in the routeros_app example.
resource "routeros_app_restart" "worker" {
  numbers = routeros_app.worker.id
  triggers = {
    request = "restart-1" # Change explicitly to restart again.
  }
}
