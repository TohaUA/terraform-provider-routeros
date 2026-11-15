resource "routeros_ip_tftp_settings" "tftp_settings" {
  max_block_size = 4096

  # RouterOS 7.25+
  vrf = "main"
}
