.PHONY: fetch generate refresh test build clean tidy

# Refresh all URL-based source snapshots under generator/sources.
# Manual specs are skipped by the generator.
fetch:
	cd generator && go run . fetch all

# Generate openapi3.json/yaml deterministically from local source snapshots.
generate:
	cd generator && go run . --output-dir .. merge
	@echo "Generation complete. Review changes with: git diff --stat"

# Refresh upstream source snapshots, then regenerate the unified artifacts.
refresh: fetch
	$(MAKE) generate

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
