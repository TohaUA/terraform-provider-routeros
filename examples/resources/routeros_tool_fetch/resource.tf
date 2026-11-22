# RouterOS 7.25.1+. Check the menu's package and hardware requirements.
# Executes on apply; a changed trigger runs it again. Destroy cannot undo it.
resource "routeros_tool_fetch" "example" {
  url                     = "https://example.com/status"
  output                  = "user"
  check_certificate       = "yes"
  sftp_known_hosts_ignore = false
  triggers = {
    request = "status-check-1"
  }
}

output "result" {
  value     = routeros_tool_fetch.example.result_json
  sensitive = true
}
