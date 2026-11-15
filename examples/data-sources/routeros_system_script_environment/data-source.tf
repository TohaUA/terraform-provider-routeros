# RouterOS 7.25+. Reads global script variables visible to the API user.
data "routeros_system_script_environment" "example" {}

output "globals" {
  value     = data.routeros_system_script_environment.example.data
  sensitive = true
}
