# One Terraform state per router

Use one Terraform root and one state key for each independently managed router.
Reuse a child module for configuration shared across routers. Each plan, apply,
and lock then belongs to a single router. A shared change requires running
Terraform in each affected root.

## Example layout

```text
examples/one-state-per-router/
├── branch-a/main.tf
├── branch-b/main.tf
└── modules/router/main.tf
```

The two roots call the same module and each targets its own RouterOS HTTPS
endpoint. Both roots use the same S3 bucket with distinct keys.

| Root | Provider endpoint | S3 state key |
| --- | --- | --- |
| `branch-a` | `https://branch-a.example` | `routers/branch-a/terraform.tfstate` |
| `branch-b` | `https://branch-b.example` | `routers/branch-b/terraform.tfstate` |

[Example files](../../examples/one-state-per-router)

Replace the example bucket, region, endpoints, and monitoring addresses.
Install the pinned fork version through the
[filesystem mirror](https://github.com/TohaUA/terraform-provider-routeros#installing-the-fork).
The bucket must exist before initialization. Configure backend credentials
separately from the RouterOS credentials. The example enables S3 lockfiles with
`use_lockfile = true`. Give each run access to its own state and lock objects.
With MinIO or another S3-compatible service, configure
its endpoint and confirm lockfile behavior on that backend before relying on it.
[Terraform S3 backend](https://developer.hashicorp.com/terraform/language/backend/s3)

## Roll out a shared change

Load branch A's `ROS_PASSWORD` into the environment for its Terraform run.
Set `ROS_CA_CERTIFICATE` to the CA bundle if the server uses a private issuer.
Run the following commands from the repository root, reviewing the plan before applying.

```sh
terraform -chdir=examples/one-state-per-router/branch-a init
terraform -chdir=examples/one-state-per-router/branch-a plan -out=rollout.tfplan
terraform -chdir=examples/one-state-per-router/branch-a show rollout.tfplan
terraform -chdir=examples/one-state-per-router/branch-a apply rollout.tfplan
```

Verify management connectivity and the changed configuration on branch A before
moving on. Load branch B's credentials and, if needed, its CA bundle. Repeat the
workflow for branch B.

```sh
terraform -chdir=examples/one-state-per-router/branch-b init
terraform -chdir=examples/one-state-per-router/branch-b plan -out=rollout.tfplan
terraform -chdir=examples/one-state-per-router/branch-b show rollout.tfplan
terraform -chdir=examples/one-state-per-router/branch-b apply rollout.tfplan
```

Keep each saved plan with its root and protect it as you would its state.
To adopt configuration that already exists on a router, use
[the import workflow](easy_import.md) in that router's root.
