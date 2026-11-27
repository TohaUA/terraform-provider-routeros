resource "routeros_interface_wireguard" "test_wg_interface" {
  name        = "test_wg_interface"
  listen_port = "13231"
}

resource "routeros_interface_wireguard_peer" "wg_peer" {
  interface  = routeros_interface_wireguard.test_wg_interface.name
  public_key = "MY_BASE_64_PUBLIC_KEY"
  allowed_address = [
    "192.168.0.0/16",
    "172.16.0.0/12",
    "10.0.0.0/8",
  ]

  # Optional write-only preshared key (increment preshared_key_wo_version to rotate):
  # preshared_key            = "BASE64_PRESHARED_KEY"
  # preshared_key_wo_version = 1

  # RouterOS 7.25.1+: MTU in the exported client configuration.
  client_mtu = 1380
}
