# RouterOS 7.25.1+. Requires the corresponding hardware/package.
data "routeros_interface_lte_show_capabilities" "example" {
  arguments = {
    numbers = "lte1"
  }
}
