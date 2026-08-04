.PHONY: regen fetch merge test build clean tidy

# ─── Regen pipeline ─────────────────────────────────────────────────────────
# Re-merge raw upstream specs into openapi3.json/yaml at the repo root.
# Run whenever upstream API specs change; then run generate-client
# in the unified-go-client sibling repo to regenerate the Go client.
regen: merge
	@echo "Regen complete. Review changes with: git diff --stat"

# Refresh all URL-based source snapshots under generator/sources.
# Manual specs are skipped by the generator.
fetch:
	cd generator && go run . fetch all

# Merge + annotate individual specs into the unified OpenAPI JSON.
# Writes openapi3.json and openapi3.yaml to the repo root.
merge:
	cd generator && go run . --output-dir .. merge

# ─── Development ────────────────────────────────────────────────────────────
test:
	cd generator && go test ./...

build:
	cd generator && go build ./...

# ─── Maintenance ────────────────────────────────────────────────────────────
tidy:
	cd generator && go mod tidy

clean:
	rm -f generator/generator
