# If you need to add a reference to an existing configuration, each inline section contains a `config` parameter 
# where you can specify the name of the actual resource.
# configuration = {
#   config = routeros_wifi_configuration.my-config.name
# }
resource "routeros_wifi" "wifi1" {
  configuration = {
    manager = "capsman"
  }
  name = "wifi1"
}

# Logical MLD and affiliation examples: see the WiFi 7 guide.
# Existing physical interfaces must be imported before management.
# New logical MLD interfaces can specify mld_name and a unique mac_address.
# Affiliated interfaces can set mld_interface to the MLD interface name.
