# unified-openapi

Unified Flexera One OpenAPI specification and the generator tool that builds it.

## Experimental

This project is currently considered **experimental** and is in the `v0.x` stage of development.

We strive to maintain compatibility where practical, but breaking changes may occur as we refine APIs, data models, workflows, and implementation details. **Until a `v1.0.0` release is published, backward compatibility should not be assumed.**

We encourage feedback and early adoption, but recommend pinning to specific versions and reviewing release notes before upgrading.

## For Maintainers and Contributors

### Layout

```
unified-openapi/
├── openapi3.json     ← the unified Flexera One OpenAPI spec (primary artifact)
├── openapi3.yaml     ← YAML companion
└── generator/        ← Go tool that merges upstream specs into openapi3.json
    ├── go.mod
    ├── main.go
    ├── merge.go
    ├── specs.yaml      ← spec source config (hand-maintained)
    └── sources/        ← per-service upstream spec snapshots (see below)
```

#### Source spec snapshots

`generator/sources/` holds committed snapshots of each upstream spec.
URL-based sources are fetched from locations defined in `generator/specs.yaml`;
manual sources are maintained directly in the repository.
To refresh a source to its latest upstream version, run:

```sh
cd generator && go run . fetch <spec-id>
# or: go run . fetch all
```

### Regenerating the spec

Use the workflow that matches the change you intend to make:

| Command | Updates upstream OpenAPI snapshots | Updates `openapi3.json` / `.yaml` | Use when |
|---------|--------------------------|-----------------------------------|----------|
| `make refresh` | Yes | Yes | You want to fetch and generate upstream specs and regenerate `openapi3.json` / `.yaml` artifacts |
| `make fetch` | Yes | No | You want to fetch upstream OpenAPI source changes without regenerating `openapi3.json` / `.yaml` artifacts |
| `make generate` | No | Yes | You want to regenerate `openapi3.json` / `.yaml` artifacts after making changes to the generator code, configuration, or committed upstream OpenAPI specs |

### Change reporting

`make fetch`, `make generate`, and `make refresh` print a structural change
summary after successful execution. `make refresh` prints one combined report.
Reports compare against `HEAD` and include preexisting local edits; use
`make changes BASE_REF=<ref>` to select another baseline without fetching or
rewriting files. `CHANGE_SCOPE=all|sources|artifacts` and
`CHANGE_FORMAT=text|markdown|json` select scope and output. Set `REPORT_CHANGES=0`
to suppress automatic reporting.

“Functional” means any structural change outside OpenAPI/Swagger `example` and
`examples` metadata, including descriptions and extensions. Actual API fields
named `example` or `examples` remain significant. Formatting and key order do
not count. Text/Markdown summaries contain only functional changes, never
example-update details or raw payloads. JSON additionally carries per-file
classification and the `has_functional_changes` flag.

The scheduled/manual `refresh-openapi-specs` workflow targets the default branch,
compares all source snapshots and both unified artifacts, and publishes the
functional summary to its job summary and PR body. Without functional changes,
it skips PR creation/update and leaves any existing refresh PR untouched. Mixed
refreshes retain all changes (including examples) in the PR; only the summary
is filtered. Comparison errors fail the job. This is not a breaking-change check.

### Generator Development

```sh
make test       # run generator unit tests
make build      # compile the generator
make generate   # rebuild artifacts from local snapshots
make tidy       # tidy go.mod / go.sum
```

Terraform-specific overrides are maintained by the downstream
`terraform-provider-flexera` repository and are not part of this OpenAPI
spec generator.

### License

Apache 2.0 — see [LICENSE](LICENSE).
