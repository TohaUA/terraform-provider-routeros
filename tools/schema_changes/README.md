This utility is designed to quickly compare changes that have occurred in ROS of specific versions.
The output can be used for information or to create an issue.

ROS schema files are required for the utility to work. You can download the latest versions with the following command:

```
export v=7.19
curl "https://tikoci.github.io/restraml/$v/inspect.json" -o- | gzip > ros-$v.json.gz
```

Startup:

```
cd tools/schema_changes
go run . -r 7.18:7.19 -markdown
```

* ```-r``` - ROS versions to be compared (```-r 7.18:7.19```)
* ```-f``` - resource path filter (```-f bgp```)
* ```-all``` - output all changed resources, not just those present in the provider
* ```-markdown``` - output in Markdown format for copying to GitHub


## Pinned RouterOS 7.25 coverage

`routeros-7.25-api.json` pins MikroTik's official `7.24->7.25` comparison and
versioned reference property tables (source URLs and HTML SHA-256 values).
Its `target_version` is **7.25.1**; earlier RouterOS versions are unsupported.
It also records explicit runtime corrections found on CHR 7.25.1. Runtime-only
entries identify their origin separately. This inventory covers release additions;
it is not a promise that every legacy RouterOS menu is implemented.

Regenerate Go metadata without network access:

```sh
go run ./tools/schema_changes/generate_7_25.go \
  -input tools/schema_changes/routeros-7.25-api.json \
  -output routeros/routeros_7_25_catalog.go
go test ./routeros -run 'TestRouterOS725|TestAPI725|TestREST725|TestIPsecKey7251|TestBGPAddressFamilies725'
```

To audit later documentation edits, fetch fresh raw tables into an audit directory:

```sh
python3 tools/schema_changes/fetch_7_25_reference.py --output /tmp/routeros-725-reference
```

Compare `delta.json` and `reference.json` with the pinned snapshot. Review types,
read-only status, mandatory arguments, hardware conditions, secrets, and operation
semantics before editing the normalized inventory. Never infer `set` support from
an `add` table: for example, SSH known hosts only support replacement. Use
`/console/inspect` and disposable-router acceptance tests to verify behavior;
record confirmed exceptions in `runtime_overrides`. Regenerate metadata, examples,
and docs after approved source changes. The fetcher intentionally does not rewrite
provider schemas or silently absorb documentation changes.

Repeated property rows can describe different driver capabilities. Preserve these
in `api_variants` instead of retaining only the last row. For example, WiFi profile
`distance` accepts both a numeric value and `indoors`/`dynamic`; its
`numeric_alternative` flag keeps the generated validator from rejecting meters.
Do not treat enum membership as proof that every radio supports that value.

`routeros-7.25-features.json` tracks release-note features outside the CLI name
delta, their validation, and unresolved hardware contracts. It also pins live
`/console/inspect` App arguments; `TestAppCapturedContract` checks both missing and
invented writable fields against that independent inventory. A feature marked
`unverified-contract` is not implemented or certified. Bluetooth output power
currently has that status. Keep RouterOS 7.26 work outside this 7.25.1 inventory.
