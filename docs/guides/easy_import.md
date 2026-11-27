# Import existing RouterOS configuration

Use import to adopt a RouterOS object into an existing Terraform root. Follow
the [fork installation instructions](https://github.com/TohaUA/terraform-provider-routeros#installing-the-fork),
initialize the root, and confirm its backend key and provider endpoint before
importing. Import binds the object to the resource address in that root's state.

## Import a static firewall address-list entry

Declare the destination resource with values that match the object already
on the router.

```terraform
resource "routeros_ip_firewall_addr_list" "monitoring" {
  address = "192.0.2.10"
  list    = "monitoring"
}
```

List the existing rows and their IDs in RouterOS Terminal.

```routeros
/ip/firewall/address-list/print show-ids
```

Match both the list name and address. Choose the static entry and substitute its
`.id` for `*A`. Quote the ID so the shell passes the asterisk literally.

```sh
terraform import routeros_ip_firewall_addr_list.monitoring '*A'
terraform plan
```

The plan should show no changes when the configuration matches, or only changes
you intend to make. Resolve any unexpected replacement or deletion before
applying. Terraform expects each remote object to be bound to one state
address. If the address is already bound, inspect it before proceeding.

## Import by a unique field

Resources that use the shared importer can accept a `field=value` lookup.
For example, the same address-list entry can be found by address when that
address occurs only once in the menu.

```sh
terraform import routeros_ip_firewall_addr_list.monitoring 'address=192.0.2.10'
```

Choose one import method. The lookup must return exactly one row. An address
can appear in several lists, so use the RouterOS ID when a lookup is ambiguous.
Check the reference page for resources with a custom importer.
[Firewall address-list reference](../resources/ip_firewall_addr_list.md)

## Bulk discovery and adoption

The repository's SSH-based importer can inspect supported menus and produce
candidate configuration. Review that output and choose the resource addresses
before importing into the intended root. Write-only inputs require separate
secret configuration when Terraform should manage them.
[Importer tool](https://github.com/TohaUA/terraform-provider-routeros/tree/main/tools/importer) · [Write-only inputs](write_only_secrets.md)
