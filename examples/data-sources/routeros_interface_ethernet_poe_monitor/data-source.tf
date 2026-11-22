# RouterOS 7.25.1+. Requires the corresponding hardware/package.
data "routeros_interface_ethernet_poe_monitor" "example" {
  arguments = {
    numbers = "ether5"
  }
}
