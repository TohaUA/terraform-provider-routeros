# RouterOS 7.25+. Check the menu's package and hardware requirements.
# Executes on apply; a changed trigger runs it again. Destroy cannot undo it.
resource "routeros_system_ssh_exec" "example" {
  address            = "192.0.2.10"
  user               = "automation"
  command            = "/system/identity/print"
  known_hosts_ignore = false
  triggers = {
    request = "identity-check-1"
  }
}

output "result" {
  value     = routeros_system_ssh_exec.example.result_json
  sensitive = true
}
