# Define routeros_app.worker as in the routeros_app example.
resource "routeros_app_update" "worker" {
  numbers = routeros_app.worker.id
  triggers = {
    request = "update-1" # Change explicitly to update again.
  }
}
