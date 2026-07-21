# unified-openapi

Unified Flexera One OpenAPI specification and the generator tool that builds it.

The generated **Go client** lives in [unified-go-client](https://github.com/flexera-public/unified-go-client).

## Prerequisites

- **Go 1.23+** — required to build and run the generator
- No other runtime dependencies; all source specs are fetched from public URLs at merge time

## Layout

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

### Source spec snapshots

`generator/sources/` holds committed snapshots of each upstream spec.
All sources are fetched from publicly accessible URLs defined in `generator/specs.yaml`.
To refresh a source to its latest upstream version, run:

```sh
cd generator && go run . fetch <spec-id>
# or: go run . fetch all
```

## Regenerating the spec

After upstream API changes, re-merge and commit the updated spec:

```sh
make merge        # or: cd generator && go run . --output-dir .. merge
git diff openapi3.json openapi3.yaml
git add openapi3.json openapi3.yaml && git commit -m "chore: regen spec"
```

Then regenerate the Go client:

```sh
cd ../unified-go-client && make generate-client
```

## Development

```sh
make test    # run generator unit tests
make build   # compile the generator binary
make tidy    # tidy go.mod / go.sum
```

## Terraform provider overrides

Copy the example and activate:

```sh
cp generator/tf-overrides.yaml.example generator/tf-overrides.yaml
make merge
```

The `tf-overrides.yaml` file is gitignored. Only the `.example` is committed.

## License

Apache 2.0 — see [LICENSE](LICENSE).
