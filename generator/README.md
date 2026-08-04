# generator

Go CLI tool that fetches, normalises, merges, and annotates upstream Flexera and
RightScale OpenAPI specs into the single unified `openapi3.json` / `openapi3.yaml`
at the repository root.

## Commands

All commands are run from within this directory, or via the Makefile targets at
the repo root.

| Command | Description |
|---------|-------------|
| `go run . merge` | Merge all enabled specs into the root `openapi3.json` / `.yaml` |
| `go run . fetch <id\|all>` | Re-download source spec(s) from their upstream URLs |
| `go run . list` | List all configured spec IDs and their enabled/disabled state |
| `go run . info <id>` | Show detailed metadata for a single spec |
| `go run . validate <id\|enabled-flexera>` | Validate the merged spec (duplicate operationIDs, broken `$ref`s, invalid parameters, server variables) |

### Common flags

| Flag | Default | Description |
|------|---------|-------------|
| `--output-dir <dir>` | `..` | Directory to write `openapi3.json` / `openapi3.yaml` into |
| `--openapi_specs_dir <dir>` | `.` | Directory containing `specs.yaml` and `sources/` |

## Quick start

```sh
# From the repo root — runs the full merge pipeline:
make merge

# Or directly:
cd generator
go run . --output-dir .. merge
```

## Source spec snapshots

`sources/` contains committed snapshots of each upstream spec. All sources are
fetched from publicly accessible URLs.

To update a single source from its upstream URL:

```sh
go run . fetch flexera-policy-v1
```

To refresh all sources:

```sh
go run . fetch all
```

## Configuration

### `specs.yaml`

All spec sources, processing rules, and output paths are declared in
`specs.yaml`. The supported fields are:

```yaml
specs:
  - id: flexera-policy-v1          # unique identifier
    name: "Flexera Policy API"     # human-readable name
    vendor: flexera                # flexera | rightscale
    service: policy                # service slug (used for component prefixing)
    version: v1                    # version string
    source:
      type: url                    # url (only public URLs are supported)
      url: "https://..."           # upstream URL
    format: openapi3-json          # openapi3-json | openapi3-yaml | swagger2-json
    output:
      path: "sources/flexera/policy/v1"
      filename: "openapi.json"
    processing:
      - type: download             # fetch from URL
      - type: sed-replace          # text patch
        pattern: "OldName"
        replacement: "NewName"
        reason: "Fix duplicate typename"
```

Two additional fields control merge-time behavior:

```yaml
  - id: rightscale-bill-analysis
    vendor: rightscale
    include_in_merge: true         # opt a non-Flexera spec into `merge` (Flexera specs are always eligible)
    merge_exclude_paths:           # drop specific source paths that duplicate an existing Flexera endpoint
      - path: "/orgs/{org}/budgets"
        reason: "Migrated to Flexera Budget API: GET/POST /finops-analytics/v1/orgs/{orgId}/budgets"
```

### Processing step types

| Type | Description |
|------|-------------|
| `download` | Fetch the spec from `source.url` |
| `convert` | Convert Swagger 2 → OpenAPI 3 (`from: swagger2`, `to: openapi3`) |
| `sed-replace` | Literal string replacement; `reason` is required and documents why the patch exists |
| `unwrap-single-anyof` | Collapse `anyOf: [X]` singletons into `X` (workaround for codegen issues) |

## Terraform provider overrides

An optional `tf-overrides.yaml` (gitignored; copy from `tf-overrides.yaml.example`)
controls Terraform-specific metadata added to the merged spec:

```sh
cp tf-overrides.yaml.example tf-overrides.yaml
```

See `tf-overrides.yaml.example` for a fully annotated example covering CRUD
bindings, field renames, write-only secrets, volatile fields, and scope variables.

## Development

```sh
# From the repo root:
make test    # go test ./...
make build   # go build ./...
make tidy    # go mod tidy

# Validate the committed spec:
cd generator && go run . validate enabled-flexera
```

## What the merge pipeline does

1. Reads `specs.yaml` and loads each enabled source from `sources/`. All
   `vendor: flexera` specs are merged automatically; specs from other
   vendors (e.g. `rightscale`) are only merged if they set
   `include_in_merge: true`, once reviewed for overlap with existing
   Flexera endpoints.
2. Applies any `sed-replace` / `unwrap-single-anyof` patches from `specs.yaml`, and drops any `merge_exclude_paths` entries (endpoints superseded by an equivalent Flexera API).
3. Converts Swagger 2 sources to OpenAPI 3 via the built-in converter.
4. Normalises each service document:
   - strips per-service `servers` blocks (replaced by a single canonical server)
   - collapses OpenAPI 3.1 `anyOf: [..., {type: null}]` to `nullable: true`
   - stamps `x-go-type: interface{}` on `format: binary` value schemas
   - enriches the OIDC token endpoint with form-encoded request body
5. Prefixes all component names and operation IDs with the service slug (e.g. `Policy_`, `Budget_`) to avoid cross-spec collisions.
6. Merges all service documents into a single spec with one canonical `servers` entry.
7. Annotates each operation with CLI extensions: `x-flexera-resource`, `x-flexera-action`, `x-flexera-paginated`, `x-flexera-list-item-ref`.
8. Optionally applies Terraform provider overrides from `tf-overrides.yaml`.
9. Writes `openapi3.json` and `openapi3.yaml` to `--output-dir`.

## Adding a new API

1. Add an entry to `specs.yaml` with `type: url` source pointing to a publicly accessible URL.
2. Run `go run . fetch <id>` to download the source snapshot.
3. Run `go run . merge` to regenerate the root spec.
4. Run `go run . validate enabled-flexera` to confirm no validation errors.
5. Commit the new `sources/…/openapi.json` snapshot and the regenerated root spec.

## License

Apache 2.0 — see [LICENSE](../LICENSE).
