package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// tfOverridesRelativePath is the conventional location of the optional
// override file consumed by applyTFOverrides. The file is optional: a
// missing file is treated as "no overrides" (pure heuristic mode). A
// present file MUST validate or merge will fail loudly.
const tfOverridesRelativePath = "tf-overrides.yaml"

// supportedTFOverridesVersion is the current schema version. Loader
// rejects any other value to keep the schema evolvable.
const supportedTFOverridesVersion = 1

// tfResourceOverride captures per-resource adjustments that correct
// generator heuristics for a single x-flexera-resource. Every field is
// optional; absent fields fall through to heuristics.
//
// Keep this list of fields stable. Adding a new field requires:
//  1. Add the YAML tag below.
//  2. Add it to validResourceKeys.
//  3. Add a stamping rule in applyTFOverrides.
type tfResourceOverride struct {
	CRUD         *tfCRUDBinding `yaml:"crud,omitempty"`
	IDField      string         `yaml:"id_field,omitempty"`
	ImportID     string         `yaml:"import_id,omitempty"`
	ListResource bool           `yaml:"list_resource,omitempty"`
	Immutable    []string       `yaml:"immutable,omitempty"`
	Sensitive    []string       `yaml:"sensitive,omitempty"`
	Skip         bool           `yaml:"skip,omitempty"`

	// WriteOnly suppresses the response-overwrite step in the
	// generated hydrator for the named top-level properties. The
	// schema, body builder, plan modifiers (Sensitive,
	// RequiresReplace), and model field declarations are UNCHANGED;
	// only the line that copies the response value into the model is
	// elided. Use this for fields whose server-side representation
	// diverges from the user's input — e.g. a `credentials` map that
	// the API redacts on read, or any secret-like value the server
	// normalizes. Without this, the spurious state-vs-plan diff on
	// every refresh will combine with RequiresReplace to plan a
	// destroy+create on every step.
	WriteOnly []string `yaml:"write_only,omitempty"`

	// Volatile lists Computed attributes whose server-side value can
	// change between reads even when the user has not modified HCL.
	// Async-evaluation fields (e.g. status, error_count, total_count,
	// active_count on policy aggregates) are the canonical case. The
	// generator skips the UseStateForUnknown plan modifier on these,
	// which means: (a) plan always shows them as `(known after apply)`
	// when HCL doesn't pin them, and (b) plugin-framework allows apply
	// to produce a different value than state without raising a
	// consistency-check error. Tests on resources with volatile fields
	// typically need ExpectNonEmptyPlan: true on PlanOnly steps; the
	// override declaration is documentation-as-code that future spec
	// regen + override re-validation surfaces.
	//
	// Distinct from Sensitive (which masks the value in plan/state
	// output) and from WriteOnly (which suppresses the hydrator's
	// response-overwrite). Volatile keeps the hydrator running so the
	// state always reflects the latest server snapshot.
	Volatile []string `yaml:"volatile,omitempty"`

	// WrapEnvelope is the per-resource opt-out for automatic
	// wrap-envelope detection. The generator detects Goa-style "response
	// wraps create body" patterns automatically (see
	// Resource.WrapEnvelopeField in cmd/gentf); set `wrap_envelope:
	// false` to force-disable detection on a specific resource if it
	// ever misfires. There is no `wrap_envelope: true` value —
	// auto-detection is the contract; manually forcing it on would
	// require an additional validation pass to confirm the candidate
	// is well-formed.
	WrapEnvelope *bool `yaml:"wrap_envelope,omitempty"`

	// Rename maps spec property name (e.g. "count") to a renamed
	// Terraform attribute name (e.g. "total_count"). The rename is
	// applied TF-side only:
	//   - schema attribute key, tfsdk tag, model struct field name use
	//     the renamed value;
	//   - the unified Go client struct field name and the JSON wire name
	//     are unchanged.
	//
	// The primary use case is escaping Terraform reserved meta-argument
	// names (count, for_each, provider, lifecycle, depends_on,
	// connection, provisioner) at the resource-block level.
	Rename map[string]string `yaml:"rename,omitempty"`

	// ScopeVars remaps API path-variable names to top-level Terraform
	// attribute names used by gentf to source those vars in CRUD calls
	// and import parsing (e.g. id: dimension).
	ScopeVars map[string]string `yaml:"scope_vars,omitempty"`
}

// tfCRUDBinding pins the four canonical operationIds for a TF resource.
// Any subset is allowed; unspecified verbs fall through to heuristics.
type tfCRUDBinding struct {
	Create string `yaml:"create,omitempty"`
	Read   string `yaml:"read,omitempty"`
	Update string `yaml:"update,omitempty"`
	Delete string `yaml:"delete,omitempty"`
}

// tfOperationOverride captures per-operation adjustments. Currently
// scoped to host carve-outs (Optima, GRS). Keyed by operationId.
type tfOperationOverride struct {
	HostOverride string `yaml:"host_override,omitempty"`
}

// tfOverridesFile is the on-disk YAML schema. version MUST equal
// supportedTFOverridesVersion. Unknown top-level keys are rejected by
// yaml.v3 strict decoding (see loadTFOverrides).
type tfOverridesFile struct {
	Version    int                            `yaml:"version"`
	Resources  map[string]tfResourceOverride  `yaml:"resources,omitempty"`
	Operations map[string]tfOperationOverride `yaml:"operations,omitempty"`
}

// validResourceKeys is the closed set of YAML keys allowed under
// resources.<name>. Loader rejects unknown keys with a precise error so
// typos surface immediately rather than silently falling through to
// heuristics.
var validResourceKeys = map[string]struct{}{
	"crud":          {},
	"id_field":      {},
	"import_id":     {},
	"list_resource": {},
	"immutable":     {},
	"sensitive":     {},
	"skip":          {},
	"rename":        {},
	"scope_vars":    {},
	"write_only":    {},
	"volatile":      {},
	"wrap_envelope": {},
}

// reservedTFMetaArgs is the closed set of attribute names Terraform
// reserves at the resource-block level. Rename targets must avoid these
// names; spec properties colliding with them must be renamed via the
// override file before the provider can load.
var reservedTFMetaArgs = map[string]struct{}{
	"count":       {},
	"for_each":    {},
	"provider":    {},
	"lifecycle":   {},
	"depends_on":  {},
	"connection":  {},
	"provisioner": {},
}

var validCRUDKeys = map[string]struct{}{
	"create": {},
	"read":   {},
	"update": {},
	"delete": {},
}

var validOperationKeys = map[string]struct{}{
	"host_override": {},
}

// loadTFOverrides reads the override file at baseDir/tfOverridesRelativePath.
// A missing file returns (nil, nil) — pure heuristic mode is the default.
// A malformed file returns an error with file:line context (yaml.v3).
func loadTFOverrides(baseDir string) (*tfOverridesFile, error) {
	path := filepath.Join(baseDir, tfOverridesRelativePath)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read tf overrides %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}

	// Two-pass decode: first into a generic map for unknown-key
	// detection (yaml.v3 KnownFields enforces strictness on the typed
	// pass but we want per-section error messages). The strict pass
	// catches misspellings of top-level fields too.
	if err := validateTFOverridesUnknownKeys(data, path); err != nil {
		return nil, err
	}

	var out tfOverridesFile
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("parse tf overrides %s: %w", path, err)
	}

	if out.Version != supportedTFOverridesVersion {
		return nil, fmt.Errorf("tf overrides %s: unsupported version %d (want %d)",
			path, out.Version, supportedTFOverridesVersion)
	}
	return &out, nil
}

// validateTFOverridesUnknownKeys does a generic-map decode pass and
// rejects any key not in the closed sets above. Catches typos like
// `inmutable:` or `crud.replace:` early, before they silently no-op.
func validateTFOverridesUnknownKeys(data []byte, path string) error {
	var generic struct {
		Version    int                             `yaml:"version"`
		Resources  map[string]map[string]yaml.Node `yaml:"resources"`
		Operations map[string]map[string]yaml.Node `yaml:"operations"`
		Extra      map[string]yaml.Node            `yaml:",inline"`
	}
	if err := yaml.Unmarshal(data, &generic); err != nil {
		return fmt.Errorf("parse tf overrides %s: %w", path, err)
	}
	if len(generic.Extra) > 0 {
		keys := make([]string, 0, len(generic.Extra))
		for k := range generic.Extra {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return fmt.Errorf("tf overrides %s: unknown top-level keys: %s",
			path, strings.Join(keys, ", "))
	}
	for resName, body := range generic.Resources {
		for k := range body {
			if _, ok := validResourceKeys[k]; !ok {
				return fmt.Errorf("tf overrides %s: resources.%s.%s is not a known field (allowed: crud, id_field, import_id, list_resource, immutable, sensitive, skip, rename, scope_vars, write_only, volatile, wrap_envelope)",
					path, resName, k)
			}
		}
		if crudNode, ok := body["crud"]; ok && crudNode.Kind == yaml.MappingNode {
			for i := 0; i < len(crudNode.Content); i += 2 {
				key := crudNode.Content[i].Value
				if _, ok := validCRUDKeys[key]; !ok {
					return fmt.Errorf("tf overrides %s: resources.%s.crud.%s is not a known field (allowed: create, read, update, delete)",
						path, resName, key)
				}
			}
		}
	}
	for opID, body := range generic.Operations {
		for k := range body {
			if _, ok := validOperationKeys[k]; !ok {
				return fmt.Errorf("tf overrides %s: operations.%s.%s is not a known field (allowed: host_override)",
					path, opID, k)
			}
		}
	}
	return nil
}

// applyTFOverrides stamps x-flexera-tf-* extensions onto the unified
// document for every override entry. Runs AFTER annotateForCLI so it can
// rely on x-flexera-resource and operationId being populated.
//
// Cross-references are validated:
//   - resources.<name> must match at least one operation's
//     x-flexera-resource (otherwise the entry is dead and almost
//     certainly a typo).
//   - crud.<verb> values and operations.<op> keys must match a real
//     operationId in the spec.
//
// Idempotent: re-running on the same doc produces the same extensions.
func applyTFOverrides(doc map[string]interface{}, overrides *tfOverridesFile) error {
	if overrides == nil {
		return nil
	}

	paths, _ := doc["paths"].(map[string]interface{})
	if paths == nil {
		// Nothing to stamp onto. Treat as no-op rather than error so
		// unit tests that build minimal docs don't have to fake paths.
		return nil
	}

	// Index operations by resource and by operationId for validation +
	// stamping.
	opsByResource := map[string][]map[string]interface{}{}
	opsByID := map[string]map[string]interface{}{}
	readOpByResource := map[string]map[string]interface{}{}
	createOpByResource := map[string]map[string]interface{}{}

	for _, pathItemValue := range paths {
		pathItem, ok := pathItemValue.(map[string]interface{})
		if !ok {
			continue
		}
		for _, method := range operationMethods {
			op, ok := pathItem[method].(map[string]interface{})
			if !ok {
				continue
			}
			if id, ok := op["operationId"].(string); ok && id != "" {
				opsByID[id] = op
			}
			res, _ := op["x-flexera-resource"].(string)
			if res == "" {
				continue
			}
			opsByResource[res] = append(opsByResource[res], op)
			action, _ := op["x-flexera-action"].(string)
			switch action {
			case "get":
				// Prefer item-GET (path ends in {id}), but any get is fine.
				readOpByResource[res] = op
			case "create":
				createOpByResource[res] = op
			}
		}
	}

	// Pull the components/schemas map once for resolving body refs
	// during rename validation. Missing components is acceptable —
	// rename validation will produce a precise error if a referenced
	// schema is absent.
	components, _ := doc["components"].(map[string]interface{})
	schemasMap, _ := components["schemas"].(map[string]interface{})

	// Resource-level overrides.
	resNames := make([]string, 0, len(overrides.Resources))
	for r := range overrides.Resources {
		resNames = append(resNames, r)
	}
	sort.Strings(resNames)
	for _, resName := range resNames {
		entry := overrides.Resources[resName]
		ops := opsByResource[resName]
		nativeOps := append([]map[string]interface{}(nil), ops...)
		if len(ops) == 0 {
			// Virtual resource entries are allowed when CRUD operationIds
			// are explicitly pinned. This enables split naming contracts
			// (e.g. org-* / project-* resources) even when the upstream
			// OpenAPI uses a shared x-flexera-resource label.
			if entry.CRUD == nil || (entry.CRUD.Create == "" && entry.CRUD.Read == "" && entry.CRUD.Update == "" && entry.CRUD.Delete == "") {
				return fmt.Errorf("tf overrides: resources.%s does not match any operation's x-flexera-resource (typo? renamed in spec?)", resName)
			}
		}

		if entry.CRUD != nil {
			binding := map[string]interface{}{}
			for verb, opID := range map[string]string{
				"create": entry.CRUD.Create,
				"read":   entry.CRUD.Read,
				"update": entry.CRUD.Update,
				"delete": entry.CRUD.Delete,
			} {
				if opID == "" {
					continue
				}
				if _, ok := opsByID[opID]; !ok {
					return fmt.Errorf("tf overrides: resources.%s.crud.%s = %q does not match any operationId in the unified spec", resName, verb, opID)
				}
				binding[verb] = opID
			}
			if len(binding) > 0 {
				// Force-register the bound create/read ops in the
				// per-resource maps, regardless of whether the bound op's
				// native `x-flexera-action` matches the verb it was pinned
				// to. This is the load-bearing entry point for the Phase 14
				// PUT-as-create pattern (Credentials API): the bound
				// `create` op carries native action=replace, and without
				// this registration the subsequent `immutable` /
				// `sensitive` / `write_only` / `volatile` stampings (which
				// target createOpByResource[resName]) silently no-op. The
				// cross-label aliasing block below handles the borrowed-op
				// case; this block handles the same-label case where only
				// the action differs from the binding's verb.
				if entry.CRUD.Create != "" {
					if op, ok := opsByID[entry.CRUD.Create]; ok {
						createOpByResource[resName] = op
					}
				}
				if entry.CRUD.Read != "" {
					if op, ok := opsByID[entry.CRUD.Read]; ok {
						readOpByResource[resName] = op
					}
				}

				// Cross-label aliasing: when a `crud:` override pulls in an
				// opId whose underlying x-flexera-resource label differs from
				// this entry's resName (the canonical case is split-lifecycle
				// shapes like AWS bill-connect, where create/update live under
				// `bill-connect-aws-iam-role` but read/delete live under
				// `bill-connect-aws`), append resName to the borrowed op's
				// `x-flexera-tf-resource-aliases` list. Both regentf discovery
				// and gentf op-enumeration honor this list in addition to the
				// native `x-flexera-resource` value, so the borrowed op is
				// counted toward this resource's CRUD quad without losing its
				// native label (many-to-one safe).
				for verb, opID := range map[string]string{
					"create": entry.CRUD.Create,
					"read":   entry.CRUD.Read,
					"update": entry.CRUD.Update,
					"delete": entry.CRUD.Delete,
				} {
					_ = verb
					if opID == "" {
						continue
					}
					op := opsByID[opID]
					nativeRes, _ := op["x-flexera-resource"].(string)
					if nativeRes == resName {
						continue
					}
					// Append (idempotent) to existing alias list. Stored as
					// []interface{} for round-trip compatibility with the rest
					// of the spec marshalling.
					var aliases []interface{}
					switch v := op["x-flexera-tf-resource-aliases"].(type) {
					case []interface{}:
						aliases = v
					case []string:
						for _, s := range v {
							aliases = append(aliases, s)
						}
					}
					present := false
					for _, a := range aliases {
						if s, _ := a.(string); s == resName {
							present = true
							break
						}
					}
					if !present {
						aliases = append(aliases, resName)
					}
					op["x-flexera-tf-resource-aliases"] = aliases
					// Also register the borrowed op in the per-resource
					// create/read maps so subsequent overrides (sensitive,
					// volatile, write_only, import_id) stamp on the correct op.
					if action, _ := op["x-flexera-action"].(string); action == "get" {
						readOpByResource[resName] = op
					} else if action == "create" {
						createOpByResource[resName] = op
					}
					// Append to opsByResource[resName] too so stampOnAll loops
					// for this resource will reach the borrowed ops. Idempotent.
					found := false
					for _, existing := range opsByResource[resName] {
						if fmt.Sprintf("%p", existing) == fmt.Sprintf("%p", op) {
							found = true
							break
						}
					}
					if !found {
						opsByResource[resName] = append(opsByResource[resName], op)
					}
				}
				// Refresh the local `ops` slice for stampOnAll calls below.
				ops = opsByResource[resName]
				targetOps := nativeOps
				if len(targetOps) == 0 {
					// Virtual resource: stamp on aliased ops that are not pure
					// borrowed deletes (shared delete labels are common and
					// should not be reclassified as standalone resources).
					for _, op := range ops {
						action, _ := op["x-flexera-action"].(string)
						if action == "delete" {
							continue
						}
						targetOps = append(targetOps, op)
					}
					if len(targetOps) == 0 {
						targetOps = ops
					}
				}
				stampOnAll(targetOps, "x-flexera-tf-crud-binding", binding)
			}
		}

		if entry.IDField != "" {
			stampOnAll(ops, "x-flexera-tf-id-field", entry.IDField)
		}

		if entry.ImportID != "" {
			// Stamp on the read op when known, otherwise fall back to
			// every op for the resource (consumer picks).
			if readOp, ok := readOpByResource[resName]; ok {
				readOp["x-flexera-tf-import-id"] = entry.ImportID
			} else {
				stampOnAll(ops, "x-flexera-tf-import-id", entry.ImportID)
			}
		}

		if entry.ListResource {
			target := readOpByResource[resName]
			if target == nil && len(ops) > 0 {
				target = ops[0]
			}
			if target != nil {
				target["x-flexera-tf-list-resource"] = true
			}
		}

		if len(entry.Immutable) > 0 {
			target := createOpByResource[resName]
			if target == nil && len(ops) > 0 {
				target = ops[0]
			}
			if target != nil {
				target["x-flexera-tf-immutable"] = stringSliceToInterface(entry.Immutable)
			}
		}

		if len(entry.Sensitive) > 0 {
			target := createOpByResource[resName]
			if target == nil && len(ops) > 0 {
				target = ops[0]
			}
			if target != nil {
				target["x-flexera-tf-sensitive"] = stringSliceToInterface(entry.Sensitive)
			}
		}

		if entry.Skip {
			stampOnAll(ops, "x-flexera-tf-skip", true)
		}

		if len(entry.Rename) > 0 {
			renameMap, err := validateAndBuildRename(resName, entry, createOpByResource[resName], readOpByResource[resName], schemasMap)
			if err != nil {
				return err
			}
			stampOnAll(ops, "x-flexera-tf-rename", renameMap)
		}

		if len(entry.ScopeVars) > 0 {
			scopeMap := map[string]interface{}{}
			for k, v := range entry.ScopeVars {
				if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
					continue
				}
				scopeMap[k] = v
			}
			if len(scopeMap) > 0 {
				target := readOpByResource[resName]
				if target == nil && len(ops) > 0 {
					target = ops[0]
				}
				if target != nil {
					target["x-flexera-tf-scope-vars"] = scopeMap
				}
			}
		}

		if len(entry.WriteOnly) > 0 {
			writeOnlyList, err := validateAndBuildWriteOnly(resName, entry.WriteOnly, createOpByResource[resName], readOpByResource[resName], schemasMap)
			if err != nil {
				return err
			}
			// Stamp on the create op (mirrors immutable/sensitive). The
			// generator reads x-flexera-tf-write-only from the create op
			// when classifying attributes.
			target := createOpByResource[resName]
			if target == nil && len(ops) > 0 {
				target = ops[0]
			}
			if target != nil {
				target["x-flexera-tf-write-only"] = writeOnlyList
			}
		}

		if len(entry.Volatile) > 0 {
			volatileList, err := validateAndBuildVolatile(resName, entry.Volatile, createOpByResource[resName], readOpByResource[resName], schemasMap)
			if err != nil {
				return err
			}
			// Stamp on the read op (volatile is a read-time concept). The
			// generator reads x-flexera-tf-volatile from the read op when
			// deciding which Computed fields to skip UseStateForUnknown on.
			target := readOpByResource[resName]
			if target == nil && len(ops) > 0 {
				target = ops[0]
			}
			if target != nil {
				target["x-flexera-tf-volatile"] = volatileList
			}
		}

		if entry.WrapEnvelope != nil && !*entry.WrapEnvelope {
			// Force-disable wrap-envelope detection on the read op. Stamped
			// only when the override is explicitly false; the default (nil)
			// leaves auto-detection enabled. We deliberately do not support
			// `wrap_envelope: true` — detection is the contract.
			target := readOpByResource[resName]
			if target == nil && len(ops) > 0 {
				target = ops[0]
			}
			if target != nil {
				target["x-flexera-tf-wrap-envelope-disabled"] = true
			}
		}
	}

	// Operation-level overrides.
	opIDs := make([]string, 0, len(overrides.Operations))
	for id := range overrides.Operations {
		opIDs = append(opIDs, id)
	}
	sort.Strings(opIDs)
	for _, opID := range opIDs {
		entry := overrides.Operations[opID]
		op, ok := opsByID[opID]
		if !ok {
			return fmt.Errorf("tf overrides: operations.%s does not match any operationId in the unified spec", opID)
		}
		if entry.HostOverride != "" {
			op["x-flexera-tf-host"] = entry.HostOverride
		}
	}

	return nil
}

func stampOnAll(ops []map[string]interface{}, key string, value interface{}) {
	for _, op := range ops {
		op[key] = value
	}
}

func stringSliceToInterface(in []string) []interface{} {
	out := make([]interface{}, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

// validateAndBuildRename returns a map[string]interface{} suitable for
// JSON-marshalling onto an operation as the x-flexera-tf-rename
// extension. It enforces:
//
//  1. Source name exists on the resource (top-level of create body OR
//     read response).
//  2. Source name is not the resource's id_field (renaming `id` would
//     require threading through CRUD code paths that aren't yet
//     rename-aware; refuse for now).
//  3. Target name is not a reserved Terraform meta-argument.
//  4. Target name does not collide with another existing top-level
//     property on the resource (after rename).
//  5. Target names within the rename map are unique among themselves.
func validateAndBuildRename(resName string, entry tfResourceOverride, createOp, readOp map[string]interface{}, schemasMap map[string]interface{}) (map[string]interface{}, error) {
	// Collect the union of top-level property names across create body
	// and read response. These are the candidate rename sources and
	// also the collision domain for rename targets.
	props := map[string]struct{}{}
	addPropsFrom := func(op map[string]interface{}, key string) {
		if op == nil {
			return
		}
		var root map[string]interface{}
		if key == "requestBody" {
			root = bodySchemaResolved(op, schemasMap)
		} else {
			root = responseSchemaResolved(op, schemasMap)
		}
		if root == nil {
			return
		}
		if p, ok := root["properties"].(map[string]interface{}); ok {
			for k := range p {
				props[k] = struct{}{}
			}
		}
	}
	addPropsFrom(createOp, "requestBody")
	addPropsFrom(readOp, "response")

	// Determine the id_field for source-blacklist purposes: explicit
	// override on read op wins; otherwise default "id".
	idField := "id"
	if readOp != nil {
		if v, _ := readOp["x-flexera-tf-id-field"].(string); v != "" {
			idField = v
		}
	}

	// Sort source names so error messages are deterministic.
	srcKeys := make([]string, 0, len(entry.Rename))
	for k := range entry.Rename {
		srcKeys = append(srcKeys, k)
	}
	sort.Strings(srcKeys)

	// Build target set within the rename map for inter-rename
	// collision detection (e.g. two sources mapping to the same target).
	targetsInRename := map[string]string{}

	out := make(map[string]interface{}, len(entry.Rename))
	for _, src := range srcKeys {
		tgt := entry.Rename[src]
		if tgt == "" {
			return nil, fmt.Errorf("tf overrides: resources.%s.rename.%s: target must be non-empty", resName, src)
		}
		if _, ok := props[src]; !ok {
			return nil, fmt.Errorf("tf overrides: resources.%s.rename.%s: no such property on resource (must appear in the create body or read response top-level)", resName, src)
		}
		if src == idField {
			return nil, fmt.Errorf("tf overrides: resources.%s.rename.%s: cannot rename the id_field; rename support for id is not implemented", resName, src)
		}
		if _, reserved := reservedTFMetaArgs[tgt]; reserved {
			return nil, fmt.Errorf("tf overrides: resources.%s.rename.%s -> %q: target is a reserved Terraform meta-argument (count, for_each, provider, lifecycle, depends_on, connection, provisioner)", resName, src, tgt)
		}
		if other, dup := targetsInRename[tgt]; dup {
			return nil, fmt.Errorf("tf overrides: resources.%s.rename: %q and %q both map to target %q", resName, other, src, tgt)
		}
		targetsInRename[tgt] = src
		// Collision with an existing untouched property: tgt must not
		// equal any other property on the resource (except itself, which
		// would be a no-op rename and is harmless but rejected for
		// clarity).
		if tgt == src {
			return nil, fmt.Errorf("tf overrides: resources.%s.rename.%s -> %s: source equals target (no-op)", resName, src, tgt)
		}
		if _, exists := props[tgt]; exists {
			return nil, fmt.Errorf("tf overrides: resources.%s.rename.%s -> %q: target collides with an existing property on the resource", resName, src, tgt)
		}
		out[src] = tgt
	}
	return out, nil
}

// bodySchemaResolved returns the resolved JSON request body schema for
// an operation, following a top-level $ref into components.schemas.
// Returns nil when no JSON request body is present.
func bodySchemaResolved(op map[string]interface{}, schemas map[string]interface{}) map[string]interface{} {
	if op == nil {
		return nil
	}
	body, _ := op["requestBody"].(map[string]interface{})
	if body == nil {
		return nil
	}
	content, _ := body["content"].(map[string]interface{})
	for mt, mtVal := range content {
		if !strings.Contains(mt, "json") {
			continue
		}
		mtMap, _ := mtVal.(map[string]interface{})
		if mtMap == nil {
			continue
		}
		s, _ := mtMap["schema"].(map[string]interface{})
		return resolveSchemaRef(s, schemas)
	}
	return nil
}

// responseSchemaResolved returns the resolved JSON 200/201 response
// schema for an operation. Returns nil when no JSON response is
// present.
func responseSchemaResolved(op map[string]interface{}, schemas map[string]interface{}) map[string]interface{} {
	if op == nil {
		return nil
	}
	responses, _ := op["responses"].(map[string]interface{})
	for _, code := range []string{"200", "201"} {
		resp, _ := responses[code].(map[string]interface{})
		if resp == nil {
			continue
		}
		content, _ := resp["content"].(map[string]interface{})
		for mt, mtVal := range content {
			if !strings.Contains(mt, "json") {
				continue
			}
			mtMap, _ := mtVal.(map[string]interface{})
			if mtMap == nil {
				continue
			}
			s, _ := mtMap["schema"].(map[string]interface{})
			return resolveSchemaRef(s, schemas)
		}
	}
	return nil
}

// resolveSchemaRef returns the target of a top-level $ref or the schema
// itself when no $ref is present.
func resolveSchemaRef(s map[string]interface{}, schemas map[string]interface{}) map[string]interface{} {
	if s == nil {
		return nil
	}
	if ref, ok := s["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		if target, ok := schemas[name].(map[string]interface{}); ok {
			return target
		}
		return nil
	}
	return s
}

// validateAndBuildWriteOnly returns the []interface{} suitable for
// JSON-marshalling onto an operation as the x-flexera-tf-write-only
// extension. It enforces:
//
//  1. Each entry exists as a top-level property on the resource
//     (create body OR read response). A typo'd entry would silently
//     no-op, which is exactly the kind of error this validator is
//     here to catch early.
//  2. No duplicates within the list.
//  3. Empty/blank entries rejected.
func validateAndBuildWriteOnly(resName string, entries []string, createOp, readOp map[string]interface{}, schemasMap map[string]interface{}) ([]interface{}, error) {
	// Collect the union of top-level property names across create body
	// and read response. Same shape used by validateAndBuildRename.
	props := map[string]struct{}{}
	addPropsFrom := func(op map[string]interface{}, key string) {
		if op == nil {
			return
		}
		var root map[string]interface{}
		if key == "requestBody" {
			root = bodySchemaResolved(op, schemasMap)
		} else {
			root = responseSchemaResolved(op, schemasMap)
		}
		if root == nil {
			return
		}
		if p, ok := root["properties"].(map[string]interface{}); ok {
			for k := range p {
				props[k] = struct{}{}
			}
		}
	}
	addPropsFrom(createOp, "requestBody")
	addPropsFrom(readOp, "response")

	seen := map[string]struct{}{}
	out := make([]interface{}, 0, len(entries))
	for _, name := range entries {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("tf overrides: resources.%s.write_only: entry must be a non-empty property name", resName)
		}
		if _, dup := seen[name]; dup {
			return nil, fmt.Errorf("tf overrides: resources.%s.write_only: duplicate entry %q", resName, name)
		}
		seen[name] = struct{}{}
		if _, ok := props[name]; !ok {
			return nil, fmt.Errorf("tf overrides: resources.%s.write_only.%s: no such property on resource (must appear in the create body or read response top-level)", resName, name)
		}
		out = append(out, name)
	}
	return out, nil
}

// validateAndBuildVolatile parallels validateAndBuildWriteOnly. Shape
// rules:
//
//  1. Each entry must match a top-level property of the resource
//     (create body or read response).
//  2. No duplicates within the list.
//  3. Empty/blank entries rejected.
//
// The semantic difference: write_only suppresses the hydrator's
// response-overwrite. Volatile keeps the hydrator running but tells
// the generator to skip UseStateForUnknown on the named property, so
// plugin-framework stops asserting plan-vs-apply equality on a value
// that legitimately changes between reads.
func validateAndBuildVolatile(resName string, entries []string, createOp, readOp map[string]interface{}, schemasMap map[string]interface{}) ([]interface{}, error) {
	props := map[string]struct{}{}
	addPropsFrom := func(op map[string]interface{}, key string) {
		if op == nil {
			return
		}
		var root map[string]interface{}
		if key == "requestBody" {
			root = bodySchemaResolved(op, schemasMap)
		} else {
			root = responseSchemaResolved(op, schemasMap)
		}
		if root == nil {
			return
		}
		if p, ok := root["properties"].(map[string]interface{}); ok {
			for k := range p {
				props[k] = struct{}{}
			}
		}
	}
	addPropsFrom(createOp, "requestBody")
	addPropsFrom(readOp, "response")

	seen := map[string]struct{}{}
	out := make([]interface{}, 0, len(entries))
	for _, name := range entries {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("tf overrides: resources.%s.volatile: entry must be a non-empty property name", resName)
		}
		if _, dup := seen[name]; dup {
			return nil, fmt.Errorf("tf overrides: resources.%s.volatile: duplicate entry %q", resName, name)
		}
		seen[name] = struct{}{}
		if _, ok := props[name]; !ok {
			return nil, fmt.Errorf("tf overrides: resources.%s.volatile.%s: no such property on resource (must appear in the create body or read response top-level)", resName, name)
		}
		out = append(out, name)
	}
	return out, nil
}
