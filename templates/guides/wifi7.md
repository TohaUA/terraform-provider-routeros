# WiFi 7 configuration on RouterOS 7.25.2

The provider exposes the documented RouterOS 7.25 WiFi 7 configuration controls.
Actual MLO, 320 MHz operation, puncturing, and AFC depend on the installed driver,
radio hardware, clients, and regulatory domain. Configuration acceptance on CHR
does not certify physical radio operation.

## Existing configurations and state

WiFi 5/6/6E configurations and v2.1.0 state retain their existing attribute names,
types, defaults, IDs, import behavior, and inline maps. There is no state migration.
All added controls are optional, without provider defaults. Omitting a new control
leaves its router value unmanaged, including during unrelated updates. Explicit
`false` is sent. Setting a new optional string to `""` uses RouterOS unset; setting
a capability set to `[]` clears that match. Returning a control to `null` stops
managing it rather than resetting it.

Existing `channel`, `configuration`, `security`, and other inline maps continue
working. Named profiles now additionally expose preamble puncturing, AFC, beacon
protection, FT R0 lifetime, installation, maximum clients, and station roaming.

## Network-based MLO

Use `routeros_interface_wifi_network` with `mlo = true` for automatic network-based
MLO. `routeros_interface_wifi_network_radio` manages per-radio templates under
`/interface/wifi/network/radio`: label selectors, extra labels, country, chains,
power, decimal-kilometer distance, channel settings, and authentication types.
Use selectors matching the intended radios; labels are not resource identities.
Import radio templates by RouterOS `.id`.

Manage each radio template in one Terraform resource. Avoid overlapping
templates or managing the same radio settings through multiple Terraform resources
and manual overrides.

## Manual MLD interfaces

Create a logical MLD and reference its interface name in affiliated interfaces:

```terraform
resource "routeros_wifi" "mld" {
  name        = "mld-lan"
  mld_name    = "lan"
  mac_address = "02:00:00:00:07:01" # Choose a unique unicast MAC.
  disabled    = true
  configuration = {
    mode = "ap"
    ssid = "WiFi7"
  }
}
```

Set `mld_interface = routeros_wifi.mld.name` on compatible affiliated interfaces.
All members must have the same SSID and reside on the same device. Keep references
in Terraform so affiliates are removed before their logical MLD. For virtual
affiliates, use an existing physical `master_interface`; an MLD cannot itself be a
master interface. Import physical interfaces before managing them; creating an MLD
does not create radios. Existing hardware-interface deletion rules still apply.

MLD names and MAC addresses must be unique. CHR can create logical MLDs but cannot
allocate distinct radio-derived MACs, so use an explicit unique MAC there. MLD names
can change in place. Unsetting `mld_name` alone fails because RouterOS requires
exactly one interface kind: radio MAC, master interface, or MLD name.

## CAPsMAN, security, and capabilities

`routeros_wifi_provisioning` supports `multi_link_mode`, `supported_hw_caps`, and
`action = "use-network-config"`. Existing action values and defaults remain valid.
`routeros_wifi_cap` exposes `mld_static` and `mld_datapath` for CAP-side MLDs.

`routeros_wifi_security` supports beacon protection, FT R0 key lifetime, and
`wpa2-psk-sha2` alongside its previous authentication methods. Beacon protection
depends on the radio capability; omitting it preserves the firmware default.

`routeros_interface_wifi_radios` reads capabilities, supported channels, band/width
information, and `ml_group`. `routeros_interface_wifi_monitor` always sends `once`
and exposes MLD link ID, channel/puncturing display, and AFC status when available.
The existing registration-table data source exposes MLD interfaces/link addresses.

`routeros_interface_wifi_radio_settings` manages the documented device-specific
external-antenna and switchable-band settings. CHR rejects writes as unsupported;
these settings require corresponding hardware. Destroying this singleton leaves
its settings on the router; import it with any non-empty ID.

## Verification

Tests pin the full relevant reference and live command argument lists, rather than
only the release delta. They check v2.1.0 schema/state compatibility, omitted-input
payloads, explicit false/reset operations, and one-shot monitoring. API and REST
acceptance cover logical MLDs, profiles, provisioning, CAP controls, radio templates,
imports, and unchanged plans on 7.25.1 and 7.25.2. Physical link negotiation, failover, radio
performance, and driver/regulatory behavior remain hardware-validation tasks.

References: [WiFi](https://manual.mikrotik.com/docs/7.25/wireless/wifi/),
[radio templates](https://manual.mikrotik.com/docs/7.25/cli-reference/interface/wifi/network/radio/),
[radio capabilities](https://manual.mikrotik.com/docs/7.25/cli-reference/interface/wifi/radio/).
