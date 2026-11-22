data "routeros_apps" "custom" {
  filter = {
    custom = "true"
  }
}

# data.routeros_apps.custom.data includes sensitive YAML and environment values.
