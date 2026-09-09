# Terraform Provider RouterOS

![module testing workflow](https://github.com/GNewbury1/terraform-provider-routeros/actions/workflows/release.yml/badge.svg?branch=main)

## Using this fork

**This repository is a fork.** It is [TohaUA/terraform-provider-routeros](https://github.com/TohaUA/terraform-provider-routeros),
built on upstream [terraform-routeros/terraform-provider-routeros](https://github.com/terraform-routeros/terraform-provider-routeros)
at commit `0d8c069` (the state of upstream `main` shortly after v1.99.1). Everything below in this
README that is not in this section is upstream's documentation and applies unchanged.

### What differs from upstream

On top of `0d8c069` the fork carries six changes:

| # | Change | Commit |
|---|--------|--------|
| 1 | 802.11be bands and 320 MHz channel width in `routeros_interface_wifi_channel` / `routeros_wifi_provisioning` | `3a729af` |
| 2 | RouterOS 7.24 fields, round 1 (bridge, bridge port, ip service, dhcp, ip settings, neighbor discovery, wireguard peer, bgp) | `f4cfd64` |
| 3 | Fix: updating a wifi/capsman access-list entry no longer crashes the provider | `5cf3b98` |
| 4 | RouterOS 7.24 fields, round 2 (trust store, logging, ssh authentication) | `f748774` |
| 5 | Dependency bumps (terraform-plugin SDK/framework, `x/crypto`, grpc, logrus, fatih/color) | `89c75e2`, `c54631a`, `8552dda` |
| 6 | `tools/schema-drift`, a RouterOS-vs-provider schema comparison (see below) | `73857fc` |

**Fork versions continue upstream's numbering, so they are not upstream's versions.** The fork's
first release is `v1.100.0`, which is *not* upstream's 1.100.0 — upstream's `CHANGELOG.md` already
contains a `1.100.0` section that was never tagged, and that entry describes upstream's work, not
the fork's. When comparing, always say "fork v1.100.0" or "upstream v1.100.0" explicitly.

### Installing the fork

The fork is **not published on registry.terraform.io** — `TohaUA/routeros` returns 404 there today.
Until it is, install it as a local build and keep `source = "terraform-routeros/routeros"` in your
`required_providers`. Nothing in your state, your `.terraform.lock.hcl` or your `-debug` workflow
has to change, because `main.go` deliberately keeps `ProviderAddr = "terraform-routeros/routeros"`.

Either point Terraform at a working copy with `dev_overrides` (no lock file entry is used, and
`terraform init` is skipped for this provider):

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

…or unpack a release archive into a `filesystem_mirror` in the packed layout, which does go through
the lock file and works in CI and air-gapped environments:

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

### If the fork is ever published as `TohaUA/routeros`

Should the `TohaUA` registry namespace be published, consumers switch to it like this:

```hcl
terraform {
  required_providers {
    routeros = {
      source  = "TohaUA/routeros"
      version = ">= 1.100.0"
    }
  }
}
```

and migrate existing state and lock file with:

```bash
terraform state replace-provider \
  registry.terraform.io/terraform-routeros/routeros \
  registry.terraform.io/TohaUA/routeros
terraform init -upgrade
git add .terraform.lock.hcl && git commit -m "chore: move to the TohaUA/routeros provider"
```

Two things to know in that mode:

* Terraform lowercases the namespace on disk, so `.terraform.lock.hcl` and the state show
  `registry.terraform.io/tohaua/routeros` even though the configuration says `TohaUA/routeros`.
* The `dev_overrides` key becomes `"TohaUA/routeros"`, but `main.go` still advertises
  `terraform-routeros/routeros`, so the `TF_REATTACH_PROVIDERS` value printed by
  `terraform-provider-routeros -debug` keeps the old key and must be edited to `TohaUA/routeros`
  before it is exported.

### Verifying a release

Releases are cut by `semantic-release` + `goreleaser`, which publishes per-platform zips, a
`terraform-provider-routeros_<version>_SHA256SUMS` file, its detached signature
`..._SHA256SUMS.sig`, and `terraform-provider-routeros_<version>_manifest.json`. The signature is
made with the GPG key held in the repository secret `TERRAFORM_GPG_SIGNING_KEY`:

```bash
gpg --import <public-key>.asc
gpg --verify terraform-provider-routeros_<version>_SHA256SUMS.sig \
             terraform-provider-routeros_<version>_SHA256SUMS
sha256sum -c --ignore-missing terraform-provider-routeros_<version>_SHA256SUMS
```

The fingerprint of that key, and the ASCII-armored public key itself, will be committed here once
the first signed release has been cut; until then treat downloads as unverified and prefer building
from a pinned commit.

### Compatibility

* Terraform plugin protocol **5.0** (`terraform-registry-manifest.json`); Terraform 1.x, and
  OpenTofu on the same protocol.
* RouterOS 7.x. Upstream's tested versions are unchanged; the fields added by the fork target
  **RouterOS 7.24** and none of them have been exercised against an older release.

### Reporting problems and retiring the fork

Issues are disabled on this fork. Report bugs that also reproduce with the upstream provider to
[upstream](https://github.com/terraform-routeros/terraform-provider-routeros/issues); raise
fork-specific problems as a pull request here, or in the repository that consumes the fork. The
fork exists only to carry the six changes above until upstream merges them, and will be retired —
consumers moving back to `terraform-routeros/routeros` — once it has.

---

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
- fork only: the fields added by this fork target ROS 7.24; see [Using this fork](#using-this-fork)

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

The `dev_overrides` key is the provider source address, so it stays `"terraform-routeros/routeros"`
for this fork as well — see [Installing the fork](#installing-the-fork). The same address is what
`terraform-provider-routeros -debug` prints in `TF_REATTACH_PROVIDERS`, because `main.go` keeps
`ProviderAddr` unchanged on purpose.

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
