# RouterOS 7.25.1 support and coverage

**RouterOS 7.25.1 is the support and acceptance-test target. Earlier versions are
unsupported and are not tested in CI.**

The provider implements all additions in MikroTik's official **7.24 → 7.25 CLI
comparison**: 46 changed menus, 128 added properties, three flags, and eight added
paths. Tests independently check every addition against registered schemas, prefix
maps, operational data sources, or explicit actions. This describes release-delta
coverage, not certification of every legacy API or every hardware variant.
RouterOS protocol fixes, firmware internals, and GUI-only changes do not each
require Terraform attributes.

Named `routeros_wifi_configuration` profiles also expose `distance` (meters as a
string, `indoors`, or `dynamic`), `hw_protection_mode` (`none`, `rts-cts`, or
`cts-to-self`), and `mode = "station-pseudobridge"`. Support depends on the WiFi
driver and radio. The reference snapshot preserves driver-specific alternatives;
a numeric distance must not be rejected just because another driver lists enums.

The CLI comparison alone does not cover every release-note feature. Native Apps
lifecycle is covered separately below. Bluetooth output-power support remains
unverified: neither the frozen reference nor the live x86 IoT package exposes its
control argument; supported Bluetooth hardware is required to establish that
contract. RouterOS 7.26
additions are a separate milestone and are not included in the 7.25.1 claim.

## Apps

`routeros_app` manages custom YAML definitions; `routeros_app_settings` manages
global storage/network/registry settings. `routeros_apps` reads custom and catalog
Apps without starting them. Custom resources accept the complete service YAML,
including per-service `privileged` and `[networkRouterIP]` placeholders. They
require the container package and enabled container device mode.

Definitions default to `disabled = true`. Set it to false to start asynchronous
deployment; `status` and `running` report progress on refresh. Configure a formatted,
mounted storage disk beforehand. Terraform does not run the interactive setup
wizard, format storage, or enable device mode. Use the actual 7.25.1 attribute
`network_pvid`; `/app/add` and `/app/set` do not accept `check-certificate`.

Use `routeros_app_restart`, `routeros_app_update`, and `routeros_app_cleanup` for
explicit operations. Each requires `numbers` (an App ID/name or selector list),
executes on creation or input/trigger changes, and does nothing on refresh or
destroy. Cleanup **permanently removes App images and data**. Custom-resource
destroy calls `/app/remove`, without implicitly calling cleanup. Catalog entries
cannot be adopted by `routeros_app`; inspect them through the data source.

YAML, environment, command lines, registry URLs, and secrets can contain
credentials. Sensitive attributes remain in Terraform state; App transport bodies
are omitted from debug logs. Semantically equivalent YAML formatting does not
cause a redeployment. Import a custom App by `.id` or `name=example-worker`; import
settings with any non-empty ID. Destroying settings leaves them on the router.

For disposable-router testing, set `ROS_APP_TESTS=1` with `TF_ACC=1` to cover
disabled definitions, settings, data sources, updates, no-op plans, and imports.
`ROS_APP_RUN_TESTS=1` additionally downloads an Alpine image and exercises running
deployment, restart, update, disable, and cleanup. These opt-in suites require
container mode and prepared storage; the standard CHR CI image lacks that setup.

## New configuration resources

These resources require **RouterOS 7.25.1+** and the corresponding package/hardware.

- [`routeros_interface_eoipv6`](../resources/interface_eoipv6.md)
- [`routeros_interface_ethernet_poe`](../resources/interface_ethernet_poe.md)
- [`routeros_interface_ethernet_pon`](../resources/interface_ethernet_pon.md)
- [`routeros_interface_lte_settings`](../resources/interface_lte_settings.md)
- [`routeros_interface_pwr_line`](../resources/interface_pwr_line.md)
- [`routeros_interface_wifi_network`](../resources/interface_wifi_network.md)
- [`routeros_interface_wifi_sniffer`](../resources/interface_wifi_sniffer.md)
- [`routeros_interface_xfrm`](../resources/interface_xfrm.md)
- [`routeros_ip_cloud_back_to_home_user`](../resources/ip_cloud_back_to_home_user.md)
- [`routeros_ip_ssh_known_hosts`](../resources/ip_ssh_known_hosts.md)
- [`routeros_ipv6_dhcp_server_binding`](../resources/ipv6_dhcp_server_binding.md)
- [`routeros_port`](../resources/port.md)
- [`routeros_routing_igmp_proxy`](../resources/routing_igmp_proxy.md)
- [`routeros_routing_pimsm_igmp_interface_template`](../resources/routing_pimsm_igmp_interface_template.md)
- [`routeros_routing_pimsm_instance`](../resources/routing_pimsm_instance.md)
- [`routeros_routing_pimsm_interface_template`](../resources/routing_pimsm_interface_template.md)
- [`routeros_routing_pimsm_static_rp`](../resources/routing_pimsm_static_rp.md)
- [`routeros_system_ptp`](../resources/system_ptp.md)
- [`routeros_system_ptp_port`](../resources/system_ptp_port.md)

PoE, PON, power-line interfaces, and serial ports manage existing hardware entries
by name. Destroy only removes their Terraform state. LTE settings, WiFi sniffer
settings, and IGMP proxy settings are singletons; destroy leaves their settings
on the router. Import singletons using any non-empty ID. Import list entries by
RouterOS `.id`, or a supported `name=value` lookup.

## Operational data sources

- [`routeros_capsman_actual_interface_configuration`](../data-sources/capsman_actual_interface_configuration.md)
- [`routeros_interface_ethernet_poe_monitor`](../data-sources/interface_ethernet_poe_monitor.md)
- [`routeros_interface_lte_show_capabilities`](../data-sources/interface_lte_show_capabilities.md)
- [`routeros_interface_wifi_registration_table`](../data-sources/interface_wifi_registration_table.md)
- [`routeros_routing_pimsm_interface`](../data-sources/routing_pimsm_interface.md)
- [`routeros_routing_pimsm_neighbor`](../data-sources/routing_pimsm_neighbor.md)
- [`routeros_routing_pimsm_uib_g`](../data-sources/routing_pimsm_uib_g.md)
- [`routeros_system_resource_hardware`](../data-sources/system_resource_hardware.md)
- [`routeros_system_script_environment`](../data-sources/system_script_environment.md)

These read operational state. PoE monitoring sends `once` to avoid an indefinite
monitor. Global variables use `/system/script/environment/print`; their values
are sensitive and limited to what the API user can see.

## Explicit command actions

- [`routeros_interface_ethernet_switch_rule_reset_counters`](../resources/interface_ethernet_switch_rule_reset_counters.md)
- [`routeros_interface_ethernet_switch_rule_reset_counters_all`](../resources/interface_ethernet_switch_rule_reset_counters_all.md)
- [`routeros_interface_lte_run_modem_update`](../resources/interface_lte_run_modem_update.md)
- [`routeros_ip_cloud_back_to_home_file_settings_remove_certificate`](../resources/ip_cloud_back_to_home_file_settings_remove_certificate.md)
- [`routeros_system_ssh_exec`](../resources/system_ssh_exec.md)
- [`routeros_tool_fetch`](../resources/tool_fetch.md)

Actions execute on creation and configuration or `triggers` changes. Refresh does
not execute them. Destroy forgets state and cannot undo a counter reset, modem
firmware update, certificate removal, remote command, or file transfer. Apply an
action only when that operation is intended.

Use typed inputs when available. `arguments` supplies exact API selectors such as
`numbers` or `.id`. Arguments and `result_json` are sensitive; command transport
body logging is disabled. Results remain in Terraform state, so protect state
access. Fetch `output` and `keep_result` conflict, even with `keep_result = false`.
Removing a typed argument plans a new execution and omits that argument from
the request. Explicit `false` remains an explicit input. Changing a trigger
explicitly reruns the command; do not use a changing timestamp
unless running on every apply is intended.

## Existing configurations and API corrections

- Keep bridge `last_member_interval` and dot1x `reauth_timeout`. The provider
  translates their wire names for 7.25.1 without changing Terraform attributes.
- `routeros_ovpn_server` manages named server instances on 7.25.1. Set `name`
  and `disabled`; replace the earlier singleton `enabled` setting with the
  inverse `disabled` value. Import an existing server by its actual `.id` or
  name; the old singleton ID is not an instance ID.
- IPv6 DHCP client options retain the 7.25.1 `fqdn` default when omitted.
  An explicit empty set clears the options; an explicit set manages them.
- SSH known hosts support add/remove/print, but no set operation. Editing a
  configured field replaces the entry.
- IPsec RSA keys use `/ip/ipsec/key/rsa` while retaining the existing
  `routeros_ip_ipsec_key` resource address. `/ip/ipsec/key` is a namespace.
- PIM instances and interface templates reject `comment`. IPv6 PIM/MLD uses the
  instance AFI and separate GMP interface templates.
- BGP family validation accepts `vpnv6` and `evpn`; SNMPv3 accepts SHA256/384/512.
- VLAN `hw_offloaded` is computed status. Configure `l3_hw_offloading` instead.
- EoIPv6 DSCP accepts numeric strings or `inherit`; MTU accepts numeric strings or
  `auto`. PON `status` is configurable, unlike similarly named status flags.
- Removed MSRP menus and obsolete runtime fields are retained in the comparison
  for auditing, not exposed as new configurable fields.

## Validation and limits

Unit tests check every release addition, exact wire keys, dotted properties,
read-only status, versioned renames, command framing, HTTP methods, progress
replies, sensitive logging, action lifecycle, and provider schema validity.
CI pins both the CHR image and `ROS_VERSION` to 7.25.1.

Disposable CHR 7.25.1 tests exercise API and REST create, update, refresh, import,
and cleanup for portable menus: XFRM, EoIPv6, WiFi network profiles, DHCPv6 bindings,
SSH known hosts, RSA keys, PIM/MLD templates, bridge security, serial ports,
IGMP/LTE/TFTP settings, fetch, and the initial seven 7.25 field additions and
SNMP SHA-2 protocols. Operational data sources are read during these tests.
The disposable-CHR setup script releases `serial0` from the console before tests,
so serial-port testing requires no manual preparation. Named OpenVPN server
create/update/import/delete and explicit DHCPv6 option clearing are also tested.
VETH runs when the menu is installed; it is not excluded by RouterOS version.

CHR cannot validate physical PTP clocks, switch ACL counters, PoE/PON behavior,
power-line adapters, modem firmware/logging, WiFi radio behavior, or Back to Home
cloud operations. Those have schema/wire coverage and still need the relevant
hardware/service acceptance. Vendor reference tables are not a substitute for
hardware testing.

```sh
go test ./routeros/ ./tools/...
# Configure a disposable CHR using the ROS_* connection variables first:
TF_ACC=1 go test -timeout 30m -v ./routeros/ -run '^TestAccRouterOS725'
```

## Sources and maintenance

The pinned inventory records versioned reference URLs, HTML SHA-256 hashes,
normalized types, and confirmed runtime corrections. Its full addition list and
test coverage are in `tools/schema_changes/routeros-7.25-api.json`.
See the [snapshot regeneration workflow](../../tools/schema_changes/README.md#pinned-routeros-725-coverage).

- [Official CLI comparison](https://manual.mikrotik.com/cli-compare.json)
- [Versioned 7.25 CLI reference](https://manual.mikrotik.com/docs/7.25/cli-reference/)
- [7.25 release announcement](https://forum.mikrotik.com/t/7-25-stable-is-released/273987/1)
- [7.25.1 release fixes](https://forum.mikrotik.com/t/7-25-1-stable-is-released/274226)
