# unified-openapi

Unified Flexera One OpenAPI specification and the generator tool that builds it.

## ⚠️ Experimental

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
    ├── sources/        ← per-service upstream spec snapshots (see below)
    ├── tf-overrides.yaml.example  ← Terraform provider override config example
    └── tf-overrides.yaml          ← active overrides (copy from .example; gitignored)
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

### Generator Development

```sh
make test       # run generator unit tests
make build      # compile the generator
make generate   # rebuild artifacts from local snapshots
make tidy       # tidy go.mod / go.sum
```

### Terraform provider overrides

A downstream Terraform Provider for Flexera references [generator/tf-overrides.yaml](generator/tf-overrides.yaml) for resource and property overrides.

We plan to move this downstream to the terraform-provider-flexera repository when it's ready.

### License

Apache 2.0 — see [LICENSE](LICENSE).
