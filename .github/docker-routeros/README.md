# RouterOS test container

Builds the RouterOS CHR image that the acceptance tests in `.github/workflows/module_testing.yml`
run against. It boots MikroTik's Cloud Hosted Router disk image under qemu inside an Alpine
container and exposes the API and REST ports.

## Why this lives here

The tests used to pull `vaerhme/routeros:v<version>` from Docker Hub. That image is published
only up to **v7.22**, and this fork tests against **7.24**, so there was nothing to pull.

Building here rather than publishing our own image keeps the version in one file, avoids
redistributing MikroTik's disk image under this account, and — the part that actually matters —
means a pull request that bumps the version proves the image boots *in the same run that
consumes it*. A separately published image can drift from the code that depends on it with no
signal in this repository.

The cost is roughly a minute of build time per CI run, most of it downloading the CHR archive
from MikroTik.

## Upstream

Vendored from [vaerh/docker-routeros](https://github.com/vaerh/docker-routeros) at commit
`ba25a1910f00f865e5f71d089a3cff64973bd148`, which is MPL-2.0 licensed; `LICENSE` is that
project's, kept verbatim, and the maintainer attribution in the `Dockerfile` is untouched.
`scripts/` is an unmodified copy.

Three things differ from upstream:

1. `ROUTEROS_VERSION`.
2. The download is retried and written to disk before extraction instead of being piped into
   `bsdtar`. This image is built on every CI run rather than occasionally, so an intermittent
   transfer is intermittent CI — a truncated stream fails the extract and cannot be retried once
   started, and upstream's fallback to the bare `.vdi` cannot help because MikroTik publishes only
   the `.zip` and that URL answers 404.
3. `entrypoint_with_four_interfaces.sh` passes `-machine accel=kvm:tcg`. Its comment promised `-enable-kvm`
   while the command passed no acceleration flag at all, so every guest ran under software
   emulation — an order of magnitude slower, and slow enough that RouterOS sometimes had not
   finished booting before CI gave up waiting. The list has to go through `-machine`, since `-accel` takes a
   single accelerator and rejects `kvm:tcg`; both are preferred over `-enable-kvm`, which aborts on
   a host without `/dev/kvm` instead of degrading to emulation.

Everything else is byte-identical, so re-syncing stays a diff against upstream rather than a merge
of divergent trees. Items 2 and 3 are worth offering upstream.

## Changing the RouterOS version

Edit `ROUTEROS_VERSION` in `Dockerfile`. That is the only place it is written down:
`module_testing.yml` reads it back out of this file and passes it to the tests as
`ROS_VERSION`, which is what they use to decide which version-gated cases apply. Keeping
it in one place is deliberate — a build running one version while the tests believe another
makes cases skip silently or assert against the wrong behaviour, and neither failure is loud.

Check the target version exists first; not every RouterOS release ships a CHR `.vdi`:

```
curl -sI https://download.mikrotik.com/routeros/7.24/chr-7.24.vdi.zip | head -1
```

## Running it locally

```
docker build -t routeros-test .github/docker-routeros
docker run -d --name routeros \
  --cap-add=NET_ADMIN --device /dev/net/tun --device /dev/kvm \
  -p 443:443 -p 8728:8728 -p 8729:8729 \
  --entrypoint /routeros/entrypoint_with_four_interfaces.sh \
  routeros-test
docker logs -f routeros    # wait for the MikroTik banner before connecting
```

It boots to a blank-password `admin` account. `/dev/kvm` makes it much faster where it is
available; without it qemu falls back to software emulation and the boot takes appreciably
longer.
