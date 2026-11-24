# RouterOS Terraform Importer (`tools/importer`)

`tools/importer` connects to a MikroTik RouterOS device over SSH, exports its configuration with `/export terse`, resolves resource `.id` values, and writes an `autoimport-<timestamp>.tf` file containing `import` and `resource` blocks with file mode `0600`.

## Usage

```sh
export ROS_PASSWORD='your-router-password'
go run ./tools/importer \
  -host 192.168.88.1 \
  -user admin
```

If neither `ROS_PASSWORD` nor `MIKROTIK_PASSWORD` is set (or is empty), the tool prompts interactively on the terminal with echo disabled.

## Flags

| Flag | Default | Description |
|---|---|---|
| `-host` | `192.168.180.171` | MikroTik host or `host:port` (IPv6 literals supported). |
| `-user` | `admin` | MikroTik SSH username. |
| `-known-hosts` | `~/.ssh/known_hosts` | Path to an OpenSSH `known_hosts` file used to verify the router's SSH host key. |
| `-host-key-fingerprint` | `""` | Expected OpenSSH SHA-256 host key fingerprint (`SHA256:...`). Mutually exclusive with `-insecure-ignore-host-key`. |
| `-insecure-ignore-host-key` | `false` | Disable SSH host key verification (unsafe; prints a warning to stderr). Mutually exclusive with `-host-key-fingerprint`. |
| `-loglevel` | `info` | Log level (`debug`, `info`, `warn`, `error`, `fatal`, `panic`). |

## Environment Variables

- `ROS_PASSWORD`: SSH login password (checked first).
- `MIKROTIK_PASSWORD`: Fallback SSH login password if `ROS_PASSWORD` is unset or empty.

## SSH Host Key Verification

By default, `tools/importer` verifies the router's SSH host key against `-known-hosts` (`~/.ssh/known_hosts`) before authenticating. You can alternatively pin the exact key with `-host-key-fingerprint SHA256:...`:

```sh
ssh-keyscan -t ed25519 192.168.88.1 >> ~/.ssh/known_hosts
# Or inspect the fingerprint first and pass -host-key-fingerprint:
ssh-keyscan -t ed25519 192.168.88.1 | ssh-keygen -lf -
```

Use `-insecure-ignore-host-key` only in trusted, isolated lab environments.
