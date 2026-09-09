# Terraform Provider RouterOS

![module testing workflow](https://github.com/GNewbury1/terraform-provider-routeros/actions/workflows/release.yml/badge.svg?branch=main)

**Note**: In release 1.43, the resource schemas have been changed:
* `routeros_routing_bgp_connection`
* `routeros_ipv6_neighbor_discovery`
* `routeros_interface_wireguard_peer`

For the first two to work correctly, you must remove the resource state (`terraform state rm <name>`) and import it again (`terraform import [options] <name> <id>`).

## Purpose

This provider allows you to configure Mikrotik routers using [old API](https://help.mikrotik.com/docs/display/ROS/API) or [REST API](https://help.mikrotik.com/docs/display/ROS/REST+API), using or not using TLS.
Compatibility testing is only performed within ROS version 7.x.

From version 1.0.0, the provider has been rewritten by [vaerh](https://github.com/vaerh), and their [fork](https://github.com/vaerh/terraform-provider-routeros) has now been merged. This version drastically improves adding new endpoints to the provider, enabling significantly easier development. [vaerh](https://github.com/vaerh) has been added as a maintainer to this project.

_We are not affiliated in any way with Mikrotik or the development of RouterOS_
## Using the provider

To get started with the provider, you first need to enable the REST API on your router. [You can follow the Mikrotik documentation on this](https://help.mikrotik.com/docs/display/ROS/REST+API), but the gist is to create an SSL cert (in `/system/certificates`) and enable the `web-ssl` service (in `/ip/services`) which uses that certificate. After that, include the following in your Terraform manifests:

```terraform
terraform {
  required_providers {
    routeros = {
      source = "terraform-routeros/routeros"
    }
  }
}

provider "routeros" {
  hosturl  = "(http|https|api|apis)://my.router.local[:port]"
  username = "my_username"
  password = "my_super_secret_password"
}

```

For more in-depth documentation about each of the resources and datasources, please read the [documentation on Hashicorp's Provider registry](https://registry.terraform.io/providers/terraform-routeros/routeros/latest/docs)

### Versions tested

- go 1.24.2 and ROS 7.12, 7.15, 7.16 (stable)

## Changelog

For a detailed changelog, please see the [changelog.md](CHANGELOG.md).

## Contributing
This version of the module greatly simplifies the process of adding new resources.
You are welcome!

### Testing

You can build the provider locally to test fixes by following these intructions:
- Build and copy the provider where Terraform reads it
```
go build *.go && \
mkdir -p ~/.terraform.d/plugins/terraform.local/local/routeros/1.0.0/$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m) && \
mv main ~/.terraform.d/plugins/terraform.local/local/routeros/1.0.0/$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m)/terraform-provider-routeros_v1.0.0
```
- Change provider from 
```hcl
required_providers {
  routeros = {
    source  = "terraform-routeros/routeros"
    version = "1.85.1"
  }
}
```

to
```hcl
required_providers {
  routeros = {
    source  = "terraform.local/local/routeros"
    version = "1.0.0"
  }
}
```
- Clean your providers, init and apply
- Alternatively, you can edit/create ~/.terraformrc add add a provider installation block like:
```hcl
provider_installation {
  dev_overrides {
     "terraform-routeros/routeros" = "/path/to/your/git/clone"
  }

  direct {
  }
}
```

and then build the provider using
```
go build -o terraform-provider-routeros *.go
```
in order for Terraform to find it.

### Fixing RouterOS property drift

Sometimes RouterOS might introduce a breaking change on a property. You can easilfy contribute to the provider by following these intructions:

- Edit `routeros/mikrotik_resource_drift.yaml`. Add the resource used as well as the old property name and the new one
- Perform the generator. It should edit file `routeros/mikrotik_resource_drift.go`.
```bash
cd routeros/
go run ../tools/drift/main.go
```
- Submit your changes!

[Here](https://github.com/terraform-routeros/terraform-provider-routeros/pull/758/files) is a example of pull request.

A drift entry is only right when the property was *renamed*: the `tf` name must be the snake-case
schema attribute and the new RouterOS key must carry the same kind of value. When RouterOS replaces
a property with one of a different shape (`/ip/ssh` `always-allow-password-login` bool ->
`password-authentication` tri-state in 7.21), add the new key as its own attribute instead. A drift
entry would rename the incoming key before the schema lookup, so the new attribute would never be
read back.

### Finding schema drift against a new RouterOS release

`tools/schema-drift` compares every resource schema with what a live router returns for the
resource's menu (GET only) and lists the fields that exist on one side only. Run it once per
RouterOS release to turn "support 7.xx" into a checklist:

```bash
export ROS_HOSTURL=https://router.example ROS_USERNAME=admin ROS_PASSWORD=... ROS_CACERT=/path/ca.pem
curl -sSL https://tikoci.github.io/restraml/7.24/inspect.json -o /tmp/inspect-7.24.json   # optional oracle
make schema-drift INSPECT=/tmp/inspect-7.24.json                                          # writes schema-drift.md / .json
```

- **missing** rows are RouterOS fields the provider does not expose (add them to the resource schema);
- **read-only** rows are status fields RouterOS never accepts on `add`/`set` (add a `Computed`
  attribute or a `MetaSkipFields` entry if the warning "Field ... not found in the schema" bothers you);
- **schema-only** rows are attributes the device did not return; RouterOS omits unset properties, so
  only rows whose `writable` column is `no` are removal candidates.

See [tools/schema-drift/README.md](tools/schema-drift/README.md) for all flags, how to check a
released binary through `terraform providers schema -json`, and the exit-code contract for CI.
