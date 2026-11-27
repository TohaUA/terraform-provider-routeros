# Terraform Provider RouterOS

[![RouterOS acceptance tests](https://github.com/TohaUA/terraform-provider-routeros/actions/workflows/module_testing.yml/badge.svg?branch=main)](https://github.com/TohaUA/terraform-provider-routeros/actions/workflows/module_testing.yml)

Manage MikroTik RouterOS with Terraform over the native API or REST. This project is a fork of
[terraform-routeros/terraform-provider-routeros](https://github.com/terraform-routeros/terraform-provider-routeros)
from upstream commit `0d8c069`. Fork releases have their own version history.

## Using this fork

- **RouterOS coverage** includes the 7.25 API additions, Wi-Fi 7 and MLD configuration,
  operational data sources, and explicit command actions. [Coverage and migration guide](docs/guides/routeros_7_25.md)
- **Write-only resource inputs** are available from fork v2.4.0 with Terraform 1.11 or later.
  [Secret migration and rotation](docs/guides/write_only_secrets.md)
- **RouterOS schema inspection** compares resource schemas with RouterOS menu responses.
  [Schema-drift tool](tools/schema-drift/README.md)

### Installing the fork

Use `source = "terraform-routeros/routeros"` with the fork binaries from
[GitHub Releases](https://github.com/TohaUA/terraform-provider-routeros/releases).
The provider keeps the existing source address so switching binaries requires no provider address
change in state. Select the fork binary using one of the installation methods below.

For local development, build from a working copy and use `dev_overrides`. Terraform uses the
override for provider execution. It still resolves versions during `terraform init`, so use the
mirror setup below when initializing a root against a fork release.

```hcl
# ~/.terraformrc
provider_installation {
  dev_overrides {
    "terraform-routeros/routeros" = "/path/to/your/clone/of/this/fork"
  }
  direct {}
}
```

```bash
go build -o terraform-provider-routeros .
```

For routine use and CI, copy the release ZIPs into a `filesystem_mirror` in the packed layout.
This method uses the dependency lock file and works in air-gapped environments. Put one ZIP in
the following location for each platform.

```
<mirror>/registry.terraform.io/terraform-routeros/routeros/terraform-provider-routeros_<version>_<os>_<arch>.zip
```

```hcl
# ~/.terraformrc
provider_installation {
  filesystem_mirror {
    path    = "/path/to/mirror"
    include = ["registry.terraform.io/terraform-routeros/routeros"]
  }
  direct {
    exclude = ["registry.terraform.io/terraform-routeros/routeros"]
  }
}
```

The fork's versions (`1.100.0` and up) are not upstream's, so a lock file that still records an
upstream version such as `1.99.1` cannot be satisfied from the mirror: a plain `terraform init` fails
with "the previously-selected version 1.99.1 is no longer available". Move the lock file to the
fork with `-upgrade` (a `version` constraint in `required_providers`, if you have one, must allow
the fork's version):

```bash
terraform init -upgrade
```

That records the checksum for the platform `init` runs on only. If the same lock file is used on
other platforms (CI, colleagues), add them from the mirror, listing every platform including your
own, and commit the result:

```bash
terraform providers lock -fs-mirror=/path/to/mirror -platform=linux_amd64 -platform=darwin_arm64
git add .terraform.lock.hcl
```

### Verifying a release

Each [GitHub release](https://github.com/TohaUA/terraform-provider-routeros/releases) includes
platform ZIP files, a SHA-256 checksum list, and a detached GPG signature for that list.
Obtain the release signing public key through a trusted channel and save it as
`release-signing-key.asc` before checking the signature.
Set `version` to the downloaded release version and run these commands from the asset directory.

```bash
version=2.5.0
checksums="terraform-provider-routeros_${version}_SHA256SUMS"
gpg --import release-signing-key.asc &&
gpg --verify "${checksums}.sig" "${checksums}" &&
sha256sum -c --ignore-missing "${checksums}"
```

Without a trusted signing key, the checksum check only confirms that the download matches
the published checksum list.

### Compatibility

- The provider accepts **RouterOS 7.25 and later**. Resources added for the 7.25 release can
  require **7.25.1 or later** and specific packages or hardware.
- Acceptance CI targets **RouterOS 7.25.2** over both native API and REST.
- Use **Terraform 1.11 or later** for write-only inputs in fork v2.4.0. The provider uses
  Terraform plugin protocol 5.0.

See the [coverage and migration guide](docs/guides/routeros_7_25.md) for version and hardware details.

### Reporting problems

File fork-specific reports in the [fork issue tracker](https://github.com/TohaUA/terraform-provider-routeros/issues).
Include the fork version, RouterOS version, transport, and a minimal configuration with secrets removed.
For a bug that also reproduces with the upstream provider, use the [upstream issue tracker](https://github.com/terraform-routeros/terraform-provider-routeros/issues).

## Connecting to a router

For REST over HTTPS, assign a certificate to `www-ssl` and use an `https://` URL.
For native API over TLS, assign a certificate to `api-ssl` and use an `apis://` URL. Set
`ca_certificate` to a local CA bundle when the router's issuer is outside the system trust store.
Supply the provider password through `ROS_PASSWORD`. [RouterOS REST API](https://manual.mikrotik.com/docs/developer-guides/rest-api/) · [RouterOS native API](https://manual.mikrotik.com/docs/developer-guides/api/)

```hcl
terraform {
  required_providers {
    routeros = {
      source = "terraform-routeros/routeros"
    }
  }
}

provider "routeros" {
  hosturl        = "https://router.example"
  username       = "terraform"
  ca_certificate = "/path/to/ca.pem"
}
```

Use TLS for production management. Plaintext `http://` and `api://` expose credentials and
configuration on the network. Fork v2.5.0 adds optional leaf certificate pinning for both TLS
transports. See the [pinning setup](docs/index.md#tls-certificate-pinning) and
[fork provider reference](docs/index.md) for configuration details.

## Operator guides

- [One state per router](docs/guides/one_state_per_router.md)
- [Import existing RouterOS configuration](docs/guides/easy_import.md)
- [Migrate and rotate write-only secrets](docs/guides/write_only_secrets.md)
- [Wi-Fi 7 configuration and validation](docs/guides/wifi7.md)

## Changelog

For a detailed changelog, please see the [changelog.md](CHANGELOG.md).

## Contributing

Use the Go version in `go.mod`. To test a local binary with Terraform, follow
[Installing the fork](#installing-the-fork).

```bash
go build ./...
go test ./routeros/ ./tools/...
```

For live acceptance tests, configure `ROS_HOSTURL`, `ROS_USERNAME`, and `ROS_PASSWORD` for
a disposable RouterOS instance and run `TF_ACC=1 go test -timeout 30m -v ./routeros`.

For documentation changes, edit the relevant files under `templates/` and `examples/`, or the
Go schema descriptions for generated attributes. Run `terraform fmt -recursive examples/`
and regenerate `docs/` with GNU sed on PATH.

```bash
make docs
```

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
RouterOS release to turn "support 7.xx" into a checklist. It reads the same connection variables as
the provider: `ROS_HOSTURL`, `ROS_USERNAME`, `ROS_PASSWORD`, and optionally `ROS_CA_CERTIFICATE` and `ROS_INSECURE`:

```bash
export ROS_HOSTURL=https://router.example ROS_USERNAME=admin ROS_PASSWORD=... ROS_CA_CERTIFICATE=/path/ca.pem
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
