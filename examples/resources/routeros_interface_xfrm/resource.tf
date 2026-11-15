# RouterOS 7.25+. Check the menu's package and hardware requirements.
resource "routeros_interface_xfrm" "example" {
  name = "xfrm-ipsec"
  mtu  = 1400
}
