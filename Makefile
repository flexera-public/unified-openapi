.PHONY: fetch generate refresh changes test build clean tidy

BASE_REF ?= HEAD
CHANGE_SCOPE ?= all
CHANGE_FORMAT ?= text
REPORT_CHANGES ?= 1

# Refresh all URL-based source snapshots under generator/sources.
# Manual specs are skipped by the generator.
fetch:
	cd generator && go run . fetch all
ifeq ($(REPORT_CHANGES),1)
	@$(MAKE) --no-print-directory changes CHANGE_SCOPE=sources
endif

# Generate openapi3.json/yaml deterministically from local source snapshots.
generate:
	cd generator && go run . --output-dir .. merge
ifeq ($(REPORT_CHANGES),1)
	@$(MAKE) --no-print-directory changes CHANGE_SCOPE=artifacts
endif

# Refresh first, generate second, then print one combined report.
refresh:
	$(MAKE) --no-print-directory fetch REPORT_CHANGES=0
	$(MAKE) --no-print-directory generate REPORT_CHANGES=0
ifeq ($(REPORT_CHANGES),1)
	@$(MAKE) --no-print-directory changes CHANGE_SCOPE=all
endif

# Read-only baseline comparison; includes preexisting local edits.
changes:
	@cd generator && go run . changes --base-ref "$(BASE_REF)" --scope "$(CHANGE_SCOPE)" --format "$(CHANGE_FORMAT)"

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
