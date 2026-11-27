# Layer-2 MAC bootstrap security and two-stage workflow

Use `routeros_mndp_neighbors` and `routeros_mac_bootstrap` (or the standalone `routeros-macboot` CLI) only during **Stage 1** initial provisioning of unaddressed MikroTik RouterOS devices on a trusted, isolated layer-2 bench or provisioning VLAN.

## Security model and threat boundaries

- **EC-SRP5 PAKE password protection:** RouterOS 6.45+ and 7.x authenticate MAC-Telnet (UDP/20561) logins with EC-SRP5 over Wei25519. Your login password is not transmitted in plaintext on the wire during authentication.
- **Cleartext post-login session:** After EC-SRP5 authentication completes, the MAC-Telnet terminal stream is **unencrypted and unauthenticated**. Any active attacker on the layer-2 segment can observe or inject commands, tamper with returned certificate fingerprints, or impersonate the router.
- **Spoofed MNDP and MITM risks:** MNDP (UDP/5678) announcements are unauthenticated UDP broadcasts. Do not run MNDP-driven bootstrap on shared, untrusted, or production LANs. Always verify the target MAC address and run only on a physically isolated bench cable or dedicated provisioning VLAN.
- **Explicit opt-in:** Both `routeros_mac_bootstrap` (`allow_insecure_l2 = true`) and `routeros-macboot` (`--allow-insecure-l2`) refuse to open a MAC-Telnet session unless you explicitly opt in.
- **Out-of-band `expected_tls_fingerprint` and password rotation:**
  - When `expected_tls_fingerprint` is supplied, bootstrap fails closed before touching REST if the router presents a different SHA-256 leaf fingerprint.
  - When `expected_tls_fingerprint` is omitted (Trust-On-First-Use over cleartext L2), **rotate the administrator password over pinned HTTPS/API-SSL in Stage 2** and pin `tls_fingerprint` in your Stage 2 provider block.
- **Secret handling and debug redaction:**
  - `routeros_mac_bootstrap` generates idempotent RouterOS scripts that **never contain passwords or secrets**.
  - Prefer `password_wo` (Terraform 1.11+ write-only argument) over `password` so credentials stay out of plan and state files.
  - Logs redact passwords and credentials by default; raw hex packet dumps are emitted only when `--unsafe-raw-debug` is explicitly passed to `routeros-macboot`.

## Two-stage provisioning workflow

### Stage 1: Layer-2 bootstrap

Stage 1 discovers the unaddressed router on a local interface, assigns an IPv4 management address, creates a self-signed TLS server certificate (idempotently without rotating existing certificates), enables `www-ssl`, and verifies `/rest/system/resource` over TLS pinned to the returned SHA-256 fingerprint.

```terraform
terraform {
  required_version = ">= 1.11.0"
  required_providers {
    routeros = {
      source = "terraform-routeros/routeros"
    }
  }
}

# Stage 1 runs over local layer-2 before the router has an IP address.
provider "routeros" {}

variable "l2_interface" {
  type        = string
  description = "Local host interface connected directly to the isolated provisioning VLAN or bench."
}

variable "bootstrap_password" {
  type        = string
  sensitive   = true
  ephemeral   = true
  default     = ""
  description = "Initial RouterOS password (empty string on factory-default RouterOS)."
}

data "routeros_mndp_neighbors" "bench" {
  interface = var.l2_interface
  timeout   = "5s"
}

resource "routeros_mac_bootstrap" "router" {
  interface           = var.l2_interface
  mac_address         = data.routeros_mndp_neighbors.bench.neighbors[0].mac_address
  address             = "192.168.88.1/24"
  target_interface    = "ether2"
  username            = "admin"
  password_wo         = var.bootstrap_password
  password_wo_version = 1

  # Required acknowledgement: post-login MAC-Telnet traffic is cleartext.
  allow_insecure_l2 = true
}

output "router_address" {
  value = routeros_mac_bootstrap.router.router_address
}

output "tls_fingerprint" {
  value = routeros_mac_bootstrap.router.tls_fingerprint
}
```

### Stage 2: Pinned TLS configuration and L2 lockdown

Stage 2 connects over `https://` with `tls_fingerprint` pinned to the Stage 1 leaf certificate, rotates the administrator password (via write-only/ephemeral secrets), and disables MAC-Telnet, MAC-WinBox, MAC-Ping, and MNDP neighbor discovery.

```terraform
terraform {
  required_version = ">= 1.11.0"
  required_providers {
    routeros = {
      source = "terraform-routeros/routeros"
    }
  }
}

variable "router_address" {
  type        = string
  description = "Bootstrapped IPv4 address from Stage 1."
}

variable "tls_fingerprint" {
  type        = string
  description = "SHA-256 TLS certificate fingerprint from Stage 1."
}

variable "admin_password" {
  type        = string
  sensitive   = true
  ephemeral   = true
  description = "New rotated administrator password."
}

# Stage 2 connects exclusively over HTTPS REST pinned to the Stage 1 TLS leaf certificate.
provider "routeros" {
  hosturl         = "https://${var.router_address}"
  username        = "admin"
  password        = var.admin_password
  tls_fingerprint = var.tls_fingerprint
}

# Disable MAC-Telnet, MAC-WinBox, MAC-Ping, and MNDP neighbor discovery once HTTPS is pinned.
resource "routeros_tool_mac_server" "lockdown" {
  allowed_interface_list = "none"
}

resource "routeros_tool_mac_server_winbox" "lockdown" {
  allowed_interface_list = "none"
}

resource "routeros_tool_mac_server_ping" "lockdown" {
  enabled = false
}

resource "routeros_ip_neighbor_discovery_settings" "lockdown" {
  discover_interface_list = "none"
}
```
