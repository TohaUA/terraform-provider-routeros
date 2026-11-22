# RouterOS 7.25.1+. Check the menu's package and hardware requirements.
# Executes on apply; a changed trigger runs it again. Destroy cannot undo it.
resource "routeros_interface_ethernet_switch_rule_reset_counters_all" "example" {
  triggers = {
    request = "initial-reset"
  }
}
