data "routeros_system_device_mode" "current" {}

output "device_mode_summary" {
  value = {
    mode                 = data.routeros_system_device_mode.current.mode
    scheduler_allowed    = data.routeros_system_device_mode.current.scheduler == true
    fetch_explicit_false = data.routeros_system_device_mode.current.fetch == false
    fetch_unavailable    = data.routeros_system_device_mode.current.fetch == null
    features             = data.routeros_system_device_mode.current.features
  }
}
