# RouterOS 7.25.1+. Check the menu's package and hardware requirements.
# Executes on apply; a changed trigger runs it again. Destroy cannot undo it.
resource "routeros_interface_lte_run_modem_update" "example" {
  arguments = {
    numbers = "lte1"
  }
  triggers = {
    request = "approved-upgrade-1"
  }
}
