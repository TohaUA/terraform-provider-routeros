data "routeros_interface_wireguard_peers" "wg0" {
  filter = {
    interface = "wg0"
  }
}
