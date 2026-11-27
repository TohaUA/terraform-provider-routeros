# Migrate to write-only secrets in fork v2.4.0

Fork v2.4.0 makes resource inputs such as passwords, private keys, and
pre-shared keys write-only. Use Terraform 1.11 or later. Terraform omits the
write-only value from saved plans and state. The companion version is a
regular integer argument that Terraform tracks. See the
[Terraform write-only argument reference](https://developer.hashicorp.com/terraform/language/manage-sensitive-data/write-only).

## Upgrade an existing configuration

Keep each resource at its existing Terraform address. Check its reference page
for the exact input and version names. For example, `routeros_system_user`
keeps `password` and adds `password_wo_version`. Use a dedicated ephemeral
variable if the existing variable is also referenced by other Terraform
arguments that persist values in state.

```terraform
variable "router_user_password" {
  type      = string
  sensitive = true
  ephemeral = true
}

resource "routeros_system_user" "monitoring" {
  name                = "monitoring"
  group               = "read"
  password            = var.router_user_password
  password_wo_version = 1
}
```

Supply `TF_VAR_router_user_password` through your secret manager or CI
environment for planning and applying. Review the plan and expect the new
version setting to send the supplied password to the router on apply. Maintain
the current value through the upgrade, or coordinate the first apply as a
password rotation.

## Rotate a secret

1. Update the value supplied by your secret manager.
2. Increase the companion version, for example from `1` to `2`.
3. Review and apply the plan. Verify that the new credential works.

Changing only the secret value does not produce a plan change. Terraform also
cannot detect a secret changed directly on the router. Check the resource's
reference before clearing a secret or rotating an input that requires
replacement. For imported certificates, use the documented `import_wo_version`
workflow. [Certificate resource reference](../resources/system_certificate.md)

## Imports and historical artifacts

Import cannot populate a write-only input. When adopting an existing object,
configure the secret and its version when Terraform should start setting it.
For optional inputs, leave both unset to keep an existing secret out of the
first update.

State snapshots, backups, and saved plans from older provider versions may
still contain the previous values. Restrict access to those artifacts, expire
them according to your retention policy, and rotate affected credentials when
needed. Router-generated values such as the Wi-Fi QR code may still appear in
state. Mark outputs that expose them as `sensitive` to hide them from ordinary
CLI output and keep access to the state restricted.
