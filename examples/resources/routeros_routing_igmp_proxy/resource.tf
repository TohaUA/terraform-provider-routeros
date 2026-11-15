# RouterOS 7.25+. Check the menu's package and hardware requirements.
resource "routeros_routing_igmp_proxy" "example" {
  robustness                 = 2
  query_interval             = "2m5s"
  query_response_interval    = "10s"
  last_member_query_interval = "1s"
}
