# schema-drift

Compares the provider's resource schemas with what a live RouterOS device returns for each
resource's menu and reports the fields that exist on one side only. The device is only ever
read (`GET`); nothing is created, changed or deleted.

Use it once per RouterOS release to turn "support 7.xx" into a checklist, or in CI against a
lab router with `-fail-on-missing`.

## How it works

1. **Mapping.** Every resource declares its RouterOS menu as the `Default` of the
   `___path___` (`MetaResourcePath`) attribute, together with `___ts___` (field renames),
   `___skip___` (ignored fields) and the version-dependent rename table in
   `routeros/mikrotik_resource_drift.go`. The tool reads all of that from
   `routeros.Provider()` at run time, so the mapping cannot drift from the provider it is
   built with; `mapping_test.go` fails when a resource has no menu path. Every menu is fetched
   once. Resources that share a menu with an identical schema (legacy aliases such as
   `routeros_bridge` / `routeros_interface_bridge`) are compared once; resources with a different
   schema on the same menu (`routeros_interface_ethernet_switch` and
   `routeros_interface_ethernet_switch_crs`) are each compared against the device, so every
   resource's attributes are classified against its own schema.
2. **Attributes** come from the compiled provider by default, or from a
   `terraform providers schema -json` file (`-schema`) when you want to check a released or
   locally built binary instead of the working tree. Nested blocks are flattened to the dotted
   names RouterOS uses (`output.add_path`), `map` attributes (CAPsMAN/WiFi inline settings)
   cover every `prefix.*` key.
3. **Device.** `GET /rest<menu>` for every menu (list menus return items, settings menus a
   single object). Keys are unioned across all items; `.id`/`.nextid`/`ret` are dropped like
   the provider's deserializer does; keys seen only on `dynamic=true` items are marked.
   Transform sets, the drift map for the device's version and skip fields are applied before
   `kebab-case` is turned into `snake_case`, exactly as `MikrotikResourceDataToTerraform` does.
   Menus the device does not have (HTTP 404, or HTTP 400 "no such command" for an
   uninstalled package) are skipped with a note.
4. **Inspect tree** (optional, `-inspect`). RouterOS omits unset properties from a `GET`, so a
   field nobody has configured on the device is invisible there. With the restraml console tree
   for the device's version every `add`/`set` argument that the device did not return is run
   through the same matching; the ones without an attribute are reported as `missing` with
   source `inspect`. Positional arguments (`place-before`, `place-after`) are ignored.
5. **Classes.**

   | class | source | meaning | counts as drift |
   |---|---|---|---|
   | `missing` | `device` | device field with no provider attribute | yes (`-fail-on-missing` exits 2) |
   | `missing` | `inspect` | writable field the device did not return, no provider attribute | yes |
   | `read-only` | `device` | device field with no attribute, but RouterOS never accepts it on `add`/`set` | no |
   | `schema-only` | `schema` | provider attribute the device did not return | no (advisory) |
   | `covered` / `skipped` | `device` | matched attribute / `MetaSkipFields` entry | no |

   Menus with no items on the device skip the `schema-only` pass (every attribute would be
   reported); the `inspect` pass still runs for them.

   A device field lands in `read-only` when it is in the status list
   (`active builtin default dynamic inactive invalid running status`, none of which is an
   `add`/`set` argument in any menu of RouterOS 7.24; extend with `-status-fields`) or, with
   `-inspect`, when the restraml console tree does not list it as an `add`/`set` argument.
   `disabled` is deliberately not in the list: it is configurable in 120+ menus.

   `schema-only` is advisory because RouterOS omits unset/default properties from a `GET`;
   only rows whose `writable` column says `no` are removal candidates. Rows that are merely
   unset (`writable = yes`) are counted in the summary but hidden from the Markdown tables
   unless `-all` is given; the JSON report always carries them.

## Running

Environment: the variables the provider reads (`routeros/provider.go`), with the same precedence
(the first non-empty one in each row wins), so an environment exported for the provider works
here unchanged:

| variables | meaning |
|---|---|
| `ROS_HOSTURL`, `MIKROTIK_HOST` | `https://router` (a `/rest` suffix is tolerated) |
| `ROS_USERNAME`, `MIKROTIK_USER` | username; a read-only user is enough |
| `ROS_PASSWORD`, `MIKROTIK_PASSWORD` | password |
| `ROS_CA_CERTIFICATE`, `MIKROTIK_CA_CERTIFICATE` | PEM bundle to trust (optional) |
| `ROS_INSECURE`, `MIKROTIK_INSECURE` | `true` to skip certificate verification (optional) |

`ROS_CACERT`, the CA variable earlier versions of this tool used, is still read, after both
provider names.

```bash
# from the repository root
export ROS_HOSTURL=https://router.example ROS_USERNAME=admin ROS_PASSWORD=... ROS_CA_CERTIFICATE=/path/ca.pem
go run ./tools/schema-drift -md schema-drift.md -json schema-drift.json

# with the writability oracle for the device's version (recommended)
v=7.24; curl -sSL "https://tikoci.github.io/restraml/$v/inspect.json" -o inspect-$v.json
go run ./tools/schema-drift -inspect inspect-$v.json -md schema-drift.md

# only some resources / menus
go run ./tools/schema-drift -resources routeros_ip_service,/interface/bridge

# or through make (runs the unit tests first)
make schema-drift INSPECT=inspect-7.24.json RESOURCES=/ip/settings DRIFT_FLAGS="-all"
```

Flags:

| flag | default | meaning |
|---|---|---|
| `-schema FILE` | compiled provider | attribute source: output of `terraform providers schema -json` |
| `-inspect FILE` | none | restraml `inspect.json[.gz]`; fills the `writable` column |
| `-resources a,b` | all | resource names and/or menu paths to compare; a path selects every schema on the menu, a resource name only its own schema and aliases |
| `-status-fields a,b` | none | extra device fields to treat as read-only |
| `-md FILE` | `-` (stdout) | Markdown report (`""` to disable) |
| `-json FILE` | none | JSON report (`-` for stdout) |
| `-all` | off | include covered fields in the tables |
| `-fail-on-missing` | off | exit 2 when any field is `missing` |
| `-timeout`, `-concurrency` | `60s`, `4` | HTTP settings |

Exit codes: `0` done, `1` usage/connection error, `2` drift found (with `-fail-on-missing`).

`1` also covers a menu whose `GET` failed for any reason other than the menu being absent
(`401` after a session or ACL change, `403 not enough permissions`, `5xx`, timeout, TLS reset,
malformed body): the comparison is then incomplete, so the run fails even when nothing was
classified as missing. Both reports are still written, and the failed menus appear in the
"Skipped menus" table with a `GET failed` reason (`"failed": true` in JSON, counted as
`summary.failed`). Menus the device simply does not have (`404`, `400 no such command`) stay
plain skips and keep exit `0`. The read-only user therefore needs `read` on every compared menu;
for a slow or busy device raise `-timeout` and lower `-concurrency`.

## Checking a specific provider binary (`-schema`)

`terraform providers schema -json` only emits attribute names and flags, not the `___path___`
defaults, so the menu mapping still comes from the provider compiled into the tool. Build the
tool from the same commit as the binary; resources the tool does not know are listed under
"unknown to this provider build" instead of being guessed.

```bash
go build -o /tmp/tfros/terraform-provider-routeros .          # or any release binary
cat > /tmp/tfros/.terraformrc <<'EOF'
provider_installation {
  dev_overrides { "terraform-routeros/routeros" = "/tmp/tfros" }
  direct {}
}
EOF
mkdir -p /tmp/tfros/ws && cat > /tmp/tfros/ws/main.tf <<'EOF'
terraform {
  required_providers { routeros = { source = "terraform-routeros/routeros" } }
}
provider "routeros" { hosturl = "https://127.0.0.1" username = "x" password = "x" insecure = true }
EOF
cd /tmp/tfros/ws && TF_CLI_CONFIG_FILE=/tmp/tfros/.terraformrc terraform init -backend=false >/dev/null && \
  TF_CLI_CONFIG_FILE=/tmp/tfros/.terraformrc terraform providers schema -json > /tmp/tfros/schema.json
cd - && go run ./tools/schema-drift -schema /tmp/tfros/schema.json
```

## Reading the report

The Markdown report starts with a summary row, then one section per menu with drift
(`missing` first), the list of menus without drift, the skipped menus with the device's
reason, and resources that have no menu. Each row is

```
| RouterOS field | provider attribute | class | writable | note |
```

where `provider attribute` is the name the field would have (or has) in the schema and `note`
carries "computed-only in provider", "dynamic only", "map attribute" or the inspect verdict.
The JSON report has the same content (`menus[].fields[]`) for scripting.

A menu shared by resources with different schemas gets one entry per schema: its sections are
titled ``### `/interface/ethernet/switch` (`routeros_interface_ethernet_switch_crs`)`` and name
the other schema's resources, the "without drift" list carries the same note, and JSON entries
list them in `shared_with`. The summary's `shared menus` column (`summary.shared_menus`) counts
such menus; `menus`, `compared`, `skipped` and `failed` count each menu once.
A shared menu the device lacks, or that could not be read, keeps one entry per schema as well.
On a shared menu, `-resources` with the menu path compares every schema; with a resource name it
compares only that resource's schema and aliases, and `shared_with` still names the others.

## Limits

- A `GET` is not a complete field inventory: RouterOS omits unset properties, so `schema-only`
  over-reports and `missing` can under-report for features not configured on the device.
  Point the tool at a router that actually uses the features you care about.
- The restraml tree is captured on one device with one package set; menus absent from it
  (CAPsMAN, container, wireless, ...) get `writable = unknown`.
- Data sources are not compared.
