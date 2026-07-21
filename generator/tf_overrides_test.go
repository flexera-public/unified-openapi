package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fixtureSpec returns a minimal unified-spec-shaped doc with the
// applied-policy CRUD quad pre-stamped (mirrors what annotateForCLI
// would produce on the real spec).
func fixtureSpec() map[string]interface{} {
	mkOp := func(opID, action string) map[string]interface{} {
		return map[string]interface{}{
			"operationId":        opID,
			"x-flexera-resource": "applied-policy",
			"x-flexera-action":   action,
		}
	}
	return map[string]interface{}{
		"paths": map[string]interface{}{
			"/policy/v1/orgs/{orgId}/projects/{projectId}/applied-policies": map[string]interface{}{
				"get":  mkOp("Policy_Applied_Policy_index", "list"),
				"post": mkOp("Policy_Applied_Policy_create", "create"),
			},
			"/policy/v1/orgs/{orgId}/projects/{projectId}/applied-policies/{appliedPolicyId}": map[string]interface{}{
				"get":    mkOp("Policy_Applied_Policy_show", "get"),
				"patch":  mkOp("Policy_Applied_Policy_update", "update"),
				"delete": mkOp("Policy_Applied_Policy_delete", "delete"),
			},
		},
	}
}

func writeOverrides(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, tfOverridesRelativePath)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadTFOverrides_MissingFile_NoOp(t *testing.T) {
	dir := t.TempDir() // no file written
	got, err := loadTFOverrides(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
}

func TestLoadTFOverrides_EmptyFile_NoOp(t *testing.T) {
	dir := writeOverrides(t, "   \n\n")
	got, err := loadTFOverrides(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
}

func TestLoadTFOverrides_AppliedPolicyExample(t *testing.T) {
	body := `version: 1
resources:
  applied-policy:
    crud:
      create: Policy_Applied_Policy_create
      read:   Policy_Applied_Policy_show
      update: Policy_Applied_Policy_update
      delete: Policy_Applied_Policy_delete
    id_field: id
    import_id: "{orgId}/{projectId}/{appliedPolicyId}"
    immutable:
      - credentials
    sensitive:
      - credentials
`
	dir := writeOverrides(t, body)
	got, err := loadTFOverrides(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got == nil {
		t.Fatal("want non-nil overrides")
	}
	if got.Version != 1 {
		t.Errorf("version = %d, want 1", got.Version)
	}
	entry, ok := got.Resources["applied-policy"]
	if !ok {
		t.Fatal("applied-policy missing")
	}
	if entry.CRUD == nil || entry.CRUD.Create != "Policy_Applied_Policy_create" {
		t.Errorf("crud.create = %+v", entry.CRUD)
	}
	if !reflect.DeepEqual(entry.Immutable, []string{"credentials"}) {
		t.Errorf("immutable = %v", entry.Immutable)
	}
	if !reflect.DeepEqual(entry.Sensitive, []string{"credentials"}) {
		t.Errorf("sensitive = %v", entry.Sensitive)
	}
	if entry.ImportID != "{orgId}/{projectId}/{appliedPolicyId}" {
		t.Errorf("import_id = %q", entry.ImportID)
	}
}

func TestLoadTFOverrides_RejectsUnknownTopLevelKey(t *testing.T) {
	dir := writeOverrides(t, "version: 1\nbogus: true\n")
	_, err := loadTFOverrides(dir)
	if err == nil || !strings.Contains(err.Error(), "unknown top-level keys") {
		t.Fatalf("want unknown-top-level-keys error, got %v", err)
	}
}

func TestLoadTFOverrides_RejectsUnknownResourceField(t *testing.T) {
	body := `version: 1
resources:
  applied-policy:
    inmutable: [credentials]   # typo of immutable
`
	dir := writeOverrides(t, body)
	_, err := loadTFOverrides(dir)
	if err == nil || !strings.Contains(err.Error(), "inmutable") {
		t.Fatalf("want inmutable typo error, got %v", err)
	}
}

func TestLoadTFOverrides_RejectsUnknownCRUDVerb(t *testing.T) {
	body := `version: 1
resources:
  applied-policy:
    crud:
      replace: Policy_Applied_Policy_update   # not a known verb
`
	dir := writeOverrides(t, body)
	_, err := loadTFOverrides(dir)
	if err == nil || !strings.Contains(err.Error(), "crud.replace") {
		t.Fatalf("want crud.replace error, got %v", err)
	}
}

func TestLoadTFOverrides_RejectsBadVersion(t *testing.T) {
	dir := writeOverrides(t, "version: 99\n")
	_, err := loadTFOverrides(dir)
	if err == nil || !strings.Contains(err.Error(), "unsupported version") {
		t.Fatalf("want unsupported-version error, got %v", err)
	}
}

func TestApplyTFOverrides_AppliedPolicyStampsExtensions(t *testing.T) {
	doc := fixtureSpec()
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"applied-policy": {
				CRUD: &tfCRUDBinding{
					Create: "Policy_Applied_Policy_create",
					Read:   "Policy_Applied_Policy_show",
					Update: "Policy_Applied_Policy_update",
					Delete: "Policy_Applied_Policy_delete",
				},
				IDField:   "id",
				ImportID:  "{orgId}/{projectId}/{appliedPolicyId}",
				Immutable: []string{"credentials"},
				Sensitive: []string{"credentials"},
			},
		},
	}

	if err := applyTFOverrides(doc, overrides); err != nil {
		t.Fatalf("apply: %v", err)
	}

	paths := doc["paths"].(map[string]interface{})
	collection := paths["/policy/v1/orgs/{orgId}/projects/{projectId}/applied-policies"].(map[string]interface{})
	item := paths["/policy/v1/orgs/{orgId}/projects/{projectId}/applied-policies/{appliedPolicyId}"].(map[string]interface{})

	createOp := collection["post"].(map[string]interface{})
	readOp := item["get"].(map[string]interface{})
	deleteOp := item["delete"].(map[string]interface{})

	// id_field stamps onto every op for the resource.
	for _, op := range []map[string]interface{}{createOp, readOp, deleteOp} {
		if op["x-flexera-tf-id-field"] != "id" {
			t.Errorf("x-flexera-tf-id-field missing or wrong: %+v", op)
		}
		binding, ok := op["x-flexera-tf-crud-binding"].(map[string]interface{})
		if !ok {
			t.Errorf("crud-binding missing on %s", op["operationId"])
			continue
		}
		if binding["create"] != "Policy_Applied_Policy_create" {
			t.Errorf("crud-binding.create = %v", binding["create"])
		}
	}

	// import_id stamps on the read op only.
	if readOp["x-flexera-tf-import-id"] != "{orgId}/{projectId}/{appliedPolicyId}" {
		t.Errorf("import_id on read = %v", readOp["x-flexera-tf-import-id"])
	}
	if _, present := createOp["x-flexera-tf-import-id"]; present {
		t.Error("import_id should NOT be on create op")
	}

	// immutable + sensitive stamp on the create op.
	imm, ok := createOp["x-flexera-tf-immutable"].([]interface{})
	if !ok || len(imm) != 1 || imm[0] != "credentials" {
		t.Errorf("immutable on create = %v", createOp["x-flexera-tf-immutable"])
	}
	sens, ok := createOp["x-flexera-tf-sensitive"].([]interface{})
	if !ok || len(sens) != 1 || sens[0] != "credentials" {
		t.Errorf("sensitive on create = %v", createOp["x-flexera-tf-sensitive"])
	}

	// Idempotency.
	if err := applyTFOverrides(doc, overrides); err != nil {
		t.Fatalf("apply (2nd): %v", err)
	}
	imm2, _ := createOp["x-flexera-tf-immutable"].([]interface{})
	if len(imm2) != 1 {
		t.Errorf("non-idempotent immutable: %v", imm2)
	}
}

func TestApplyTFOverrides_ScopeVarsStampOnReadOp(t *testing.T) {
	doc := fixtureSpec()
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"applied-policy": {
				ScopeVars: map[string]string{
					"appliedPolicyId": "applied_policy_ref",
				},
			},
		},
	}
	if err := applyTFOverrides(doc, overrides); err != nil {
		t.Fatalf("apply: %v", err)
	}

	paths := doc["paths"].(map[string]interface{})
	item := paths["/policy/v1/orgs/{orgId}/projects/{projectId}/applied-policies/{appliedPolicyId}"].(map[string]interface{})
	collection := paths["/policy/v1/orgs/{orgId}/projects/{projectId}/applied-policies"].(map[string]interface{})
	readOp := item["get"].(map[string]interface{})
	createOp := collection["post"].(map[string]interface{})

	raw, ok := readOp["x-flexera-tf-scope-vars"].(map[string]interface{})
	if !ok {
		t.Fatalf("x-flexera-tf-scope-vars missing on read op")
	}
	if got := raw["appliedPolicyId"]; got != "applied_policy_ref" {
		t.Fatalf("scope var map value = %v, want applied_policy_ref", got)
	}
	if _, ok := createOp["x-flexera-tf-scope-vars"]; ok {
		t.Fatalf("x-flexera-tf-scope-vars should not be stamped on create op")
	}
}

func TestApplyTFOverrides_NilOverrides_NoOp(t *testing.T) {
	doc := fixtureSpec()
	before := lenExtensions(doc)
	if err := applyTFOverrides(doc, nil); err != nil {
		t.Fatal(err)
	}
	if got := lenExtensions(doc); got != before {
		t.Errorf("nil overrides should not stamp anything, before=%d after=%d", before, got)
	}
}

func TestApplyTFOverrides_UnknownResource_Errors(t *testing.T) {
	doc := fixtureSpec()
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"not-a-real-resource": {IDField: "id"},
		},
	}
	err := applyTFOverrides(doc, overrides)
	if err == nil || !strings.Contains(err.Error(), "not-a-real-resource") {
		t.Fatalf("want unknown-resource error, got %v", err)
	}
}

func TestApplyTFOverrides_UnknownOpID_Errors(t *testing.T) {
	doc := fixtureSpec()
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"applied-policy": {
				CRUD: &tfCRUDBinding{Create: "Policy_Wrong_OpId"},
			},
		},
	}
	err := applyTFOverrides(doc, overrides)
	if err == nil || !strings.Contains(err.Error(), "Policy_Wrong_OpId") {
		t.Fatalf("want unknown-opId error, got %v", err)
	}
}

func TestApplyTFOverrides_Skip(t *testing.T) {
	doc := fixtureSpec()
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"applied-policy": {Skip: true},
		},
	}
	if err := applyTFOverrides(doc, overrides); err != nil {
		t.Fatal(err)
	}
	paths := doc["paths"].(map[string]interface{})
	item := paths["/policy/v1/orgs/{orgId}/projects/{projectId}/applied-policies/{appliedPolicyId}"].(map[string]interface{})
	for _, m := range []string{"get", "patch", "delete"} {
		op := item[m].(map[string]interface{})
		if op["x-flexera-tf-skip"] != true {
			t.Errorf("skip not stamped on %s", m)
		}
	}
}

func TestApplyTFOverrides_OperationHostOverride(t *testing.T) {
	doc := fixtureSpec()
	overrides := &tfOverridesFile{
		Version: 1,
		Operations: map[string]tfOperationOverride{
			"Policy_Applied_Policy_create": {HostOverride: "optima"},
		},
	}
	if err := applyTFOverrides(doc, overrides); err != nil {
		t.Fatal(err)
	}
	paths := doc["paths"].(map[string]interface{})
	createOp := paths["/policy/v1/orgs/{orgId}/projects/{projectId}/applied-policies"].(map[string]interface{})["post"].(map[string]interface{})
	if createOp["x-flexera-tf-host"] != "optima" {
		t.Errorf("host_override = %v", createOp["x-flexera-tf-host"])
	}
}

// lenExtensions counts x-flexera-tf-* keys across all ops, for no-op
// regression checks.
func lenExtensions(doc map[string]interface{}) int {
	n := 0
	paths, _ := doc["paths"].(map[string]interface{})
	for _, pi := range paths {
		item, _ := pi.(map[string]interface{})
		for _, m := range operationMethods {
			op, ok := item[m].(map[string]interface{})
			if !ok {
				continue
			}
			for k := range op {
				if strings.HasPrefix(k, "x-flexera-tf-") {
					n++
				}
			}
		}
	}
	return n
}

// fixtureSpecWithSchemas returns a minimal unified-shaped doc with
// resolved component schemas attached, suitable for exercising rename
// validation (which inspects the create body + read response top-level
// properties).
func fixtureSpecWithSchemas(props []string) map[string]interface{} {
	propsMap := map[string]interface{}{}
	for _, p := range props {
		propsMap[p] = map[string]interface{}{"type": "string"}
	}
	bodySchema := map[string]interface{}{
		"type":       "object",
		"properties": propsMap,
	}
	mkOp := func(opID, action string, withBody bool) map[string]interface{} {
		op := map[string]interface{}{
			"operationId":        opID,
			"x-flexera-resource": "applied-policy",
			"x-flexera-action":   action,
		}
		if withBody {
			op["requestBody"] = map[string]interface{}{
				"content": map[string]interface{}{
					"application/json": map[string]interface{}{
						"schema": map[string]interface{}{"$ref": "#/components/schemas/Body"},
					},
				},
			}
		}
		op["responses"] = map[string]interface{}{
			"200": map[string]interface{}{
				"content": map[string]interface{}{
					"application/json": map[string]interface{}{
						"schema": map[string]interface{}{"$ref": "#/components/schemas/Body"},
					},
				},
			},
		}
		return op
	}
	return map[string]interface{}{
		"paths": map[string]interface{}{
			"/policy/v1/orgs/{orgId}/projects/{projectId}/applied-policies": map[string]interface{}{
				"post": mkOp("Policy_Applied_Policy_create", "create", true),
			},
			"/policy/v1/orgs/{orgId}/projects/{projectId}/applied-policies/{appliedPolicyId}": map[string]interface{}{
				"get":    mkOp("Policy_Applied_Policy_show", "get", false),
				"patch":  mkOp("Policy_Applied_Policy_update", "update", true),
				"delete": mkOp("Policy_Applied_Policy_delete", "delete", false),
			},
		},
		"components": map[string]interface{}{
			"schemas": map[string]interface{}{
				"Body": bodySchema,
			},
		},
	}
}

// TestApplyTFOverrides_RenameSourceMissing rejects a rename whose
// source name does not appear on the resource.
func TestApplyTFOverrides_RenameSourceMissing(t *testing.T) {
	doc := fixtureSpecWithSchemas([]string{"name", "description"})
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"applied-policy": {Rename: map[string]string{"nope": "noper"}},
		},
	}
	err := applyTFOverrides(doc, overrides)
	if err == nil || !strings.Contains(err.Error(), "rename.nope") || !strings.Contains(err.Error(), "no such property") {
		t.Fatalf("want missing-source error mentioning rename.nope, got %v", err)
	}
}

// TestApplyTFOverrides_RenameTargetCollision rejects a rename whose
// target collides with another property on the same resource.
func TestApplyTFOverrides_RenameTargetCollision(t *testing.T) {
	doc := fixtureSpecWithSchemas([]string{"name", "description", "count"})
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"applied-policy": {Rename: map[string]string{"count": "description"}},
		},
	}
	err := applyTFOverrides(doc, overrides)
	if err == nil || !strings.Contains(err.Error(), "collides") {
		t.Fatalf("want collision error, got %v", err)
	}
}

// TestApplyTFOverrides_RenameTargetReserved exercises every reserved
// Terraform meta-argument name as a rename target.
func TestApplyTFOverrides_RenameTargetReserved(t *testing.T) {
	reservedNames := []string{"count", "for_each", "provider", "lifecycle", "depends_on", "connection", "provisioner"}
	for _, target := range reservedNames {
		t.Run(target, func(t *testing.T) {
			doc := fixtureSpecWithSchemas([]string{"name", "description", "original"})
			overrides := &tfOverridesFile{
				Version: 1,
				Resources: map[string]tfResourceOverride{
					"applied-policy": {Rename: map[string]string{"original": target}},
				},
			}
			err := applyTFOverrides(doc, overrides)
			if err == nil || !strings.Contains(err.Error(), "reserved Terraform meta-argument") {
				t.Fatalf("target=%q: want reserved-meta-arg error, got %v", target, err)
			}
		})
	}
}

// TestApplyTFOverrides_RenameValid stamps x-flexera-tf-rename on every
// op for the resource when the rename validates.
func TestApplyTFOverrides_RenameValid(t *testing.T) {
	doc := fixtureSpecWithSchemas([]string{"name", "description", "count"})
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"applied-policy": {Rename: map[string]string{"count": "total_count"}},
		},
	}
	if err := applyTFOverrides(doc, overrides); err != nil {
		t.Fatalf("apply: %v", err)
	}
	paths := doc["paths"].(map[string]interface{})
	item := paths["/policy/v1/orgs/{orgId}/projects/{projectId}/applied-policies/{appliedPolicyId}"].(map[string]interface{})
	for _, m := range []string{"get", "patch", "delete"} {
		op := item[m].(map[string]interface{})
		renameRaw, ok := op["x-flexera-tf-rename"].(map[string]interface{})
		if !ok {
			t.Errorf("x-flexera-tf-rename missing on %s", m)
			continue
		}
		if got := renameRaw["count"]; got != "total_count" {
			t.Errorf("%s: rename.count = %v, want total_count", m, got)
		}
	}

	// Idempotency.
	if err := applyTFOverrides(doc, overrides); err != nil {
		t.Fatalf("apply (2nd): %v", err)
	}
}

// TestApplyTFOverrides_RenameRejectsIDField forbids renaming the
// resource's id field, since CRUD code paths read r.IDField directly
// and rename support for id is not yet implemented.
func TestApplyTFOverrides_RenameRejectsIDField(t *testing.T) {
	doc := fixtureSpecWithSchemas([]string{"id", "name"})
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"applied-policy": {Rename: map[string]string{"id": "resource_id"}},
		},
	}
	err := applyTFOverrides(doc, overrides)
	if err == nil || !strings.Contains(err.Error(), "id_field") {
		t.Fatalf("want id_field-rename rejection, got %v", err)
	}
}

// TestApplyTFOverrides_RenameRejectsNoOp rejects source==target as a
// configuration mistake (silent no-op would mask user error).
func TestApplyTFOverrides_RenameRejectsNoOp(t *testing.T) {
	doc := fixtureSpecWithSchemas([]string{"name", "description"})
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"applied-policy": {Rename: map[string]string{"name": "name"}},
		},
	}
	err := applyTFOverrides(doc, overrides)
	if err == nil || !strings.Contains(err.Error(), "no-op") {
		t.Fatalf("want no-op rejection, got %v", err)
	}
}

// TestLoadTFOverrides_AcceptsRenameKey verifies the loader passes
// `rename:` through known-fields strict decoding.
func TestLoadTFOverrides_AcceptsRenameKey(t *testing.T) {
	body := `version: 1
resources:
  applied-policy:
    rename:
      count: total_count
`
	dir := writeOverrides(t, body)
	got, err := loadTFOverrides(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got == nil {
		t.Fatal("want non-nil overrides")
	}
	entry := got.Resources["applied-policy"]
	if entry.Rename["count"] != "total_count" {
		t.Errorf("rename.count = %q, want total_count", entry.Rename["count"])
	}
}

// --- Phase 4: write_only override tests --------------------------------
//
// The write_only override flag suppresses the hydrator's response
// overwrite for a named property whose server-side representation
// diverges from the user's input (e.g. credentials maps redacted on
// read). Schema, body builder, and plan modifiers are unchanged.

// TestApplyTFOverrides_WriteOnlyAppliedPolicy stamps
// x-flexera-tf-write-only on the create op for the canonical
// applied-policy / credentials use case.
func TestApplyTFOverrides_WriteOnlyAppliedPolicy(t *testing.T) {
	doc := fixtureSpecWithSchemas([]string{"name", "description", "credentials"})
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"applied-policy": {WriteOnly: []string{"credentials"}},
		},
	}
	if err := applyTFOverrides(doc, overrides); err != nil {
		t.Fatalf("apply: %v", err)
	}
	paths := doc["paths"].(map[string]interface{})
	collection := paths["/policy/v1/orgs/{orgId}/projects/{projectId}/applied-policies"].(map[string]interface{})
	createOp := collection["post"].(map[string]interface{})
	wo, ok := createOp["x-flexera-tf-write-only"].([]interface{})
	if !ok {
		t.Fatalf("x-flexera-tf-write-only missing on create op: %+v", createOp)
	}
	if len(wo) != 1 || wo[0] != "credentials" {
		t.Errorf("write_only on create = %v, want [credentials]", wo)
	}

	// Idempotent.
	if err := applyTFOverrides(doc, overrides); err != nil {
		t.Fatalf("apply (2nd): %v", err)
	}
	wo2, _ := createOp["x-flexera-tf-write-only"].([]interface{})
	if len(wo2) != 1 {
		t.Errorf("non-idempotent write_only: %v", wo2)
	}

	// write_only stamps onto the CREATE op only (mirrors immutable/sensitive).
	item := paths["/policy/v1/orgs/{orgId}/projects/{projectId}/applied-policies/{appliedPolicyId}"].(map[string]interface{})
	deleteOp := item["delete"].(map[string]interface{})
	if _, present := deleteOp["x-flexera-tf-write-only"]; present {
		t.Errorf("write_only should NOT be on delete op")
	}
}

// TestApplyTFOverrides_WriteOnlyUnknownProperty rejects entries that
// don't appear on the resource. Catches typos before they silently
// no-op.
func TestApplyTFOverrides_WriteOnlyUnknownProperty(t *testing.T) {
	doc := fixtureSpecWithSchemas([]string{"name", "description"})
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"applied-policy": {WriteOnly: []string{"credentialz"}}, // typo
		},
	}
	err := applyTFOverrides(doc, overrides)
	if err == nil || !strings.Contains(err.Error(), "write_only.credentialz") || !strings.Contains(err.Error(), "no such property") {
		t.Fatalf("want unknown-property error mentioning write_only.credentialz, got %v", err)
	}
}

// TestApplyTFOverrides_WriteOnlyDuplicate rejects duplicate entries in
// the write_only list.
func TestApplyTFOverrides_WriteOnlyDuplicate(t *testing.T) {
	doc := fixtureSpecWithSchemas([]string{"name", "credentials"})
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"applied-policy": {WriteOnly: []string{"credentials", "credentials"}},
		},
	}
	err := applyTFOverrides(doc, overrides)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("want duplicate-entry error, got %v", err)
	}
}

// TestLoadTFOverrides_AcceptsWriteOnlyKey verifies the loader passes
// `write_only:` through known-fields strict decoding.
func TestLoadTFOverrides_AcceptsWriteOnlyKey(t *testing.T) {
	body := `version: 1
resources:
  applied-policy:
    write_only:
      - credentials
`
	dir := writeOverrides(t, body)
	got, err := loadTFOverrides(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got == nil {
		t.Fatal("want non-nil overrides")
	}
	entry := got.Resources["applied-policy"]
	if !reflect.DeepEqual(entry.WriteOnly, []string{"credentials"}) {
		t.Errorf("write_only = %v, want [credentials]", entry.WriteOnly)
	}
}

func TestLoadTFOverrides_AcceptsListResourceKey(t *testing.T) {
	body := `version: 1
resources:
  applied-policy:
    list_resource: true
`
	dir := writeOverrides(t, body)
	got, err := loadTFOverrides(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	entry := got.Resources["applied-policy"]
	if !entry.ListResource {
		t.Errorf("list_resource = %v, want true", entry.ListResource)
	}
}

func TestApplyTFOverrides_ListResourceStampsReadOp(t *testing.T) {
	doc := fixtureSpec()
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"applied-policy": {ListResource: true},
		},
	}
	if err := applyTFOverrides(doc, overrides); err != nil {
		t.Fatalf("apply: %v", err)
	}
	paths := doc["paths"].(map[string]interface{})
	readOp := paths["/policy/v1/orgs/{orgId}/projects/{projectId}/applied-policies/{appliedPolicyId}"].(map[string]interface{})["get"].(map[string]interface{})
	if v, _ := readOp["x-flexera-tf-list-resource"].(bool); !v {
		t.Fatalf("x-flexera-tf-list-resource on read op = %v, want true", readOp["x-flexera-tf-list-resource"])
	}
	createOp := paths["/policy/v1/orgs/{orgId}/projects/{projectId}/applied-policies"].(map[string]interface{})["post"].(map[string]interface{})
	if _, present := createOp["x-flexera-tf-list-resource"]; present {
		t.Fatalf("x-flexera-tf-list-resource should not be stamped on create op")
	}
}

// TestApplyTFOverrides_VolatileStampsExtension verifies that a volatile
// override entry stamps x-flexera-tf-volatile on the read op (not the
// create op — volatile is a read-time concept).
func TestApplyTFOverrides_VolatileStampsExtension(t *testing.T) {
	doc := fixtureSpecWithSchemas([]string{"description"})
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"applied-policy": {Volatile: []string{"description"}},
		},
	}
	if err := applyTFOverrides(doc, overrides); err != nil {
		t.Fatalf("apply: %v", err)
	}
	paths := doc["paths"].(map[string]interface{})
	readOp := paths["/policy/v1/orgs/{orgId}/projects/{projectId}/applied-policies/{appliedPolicyId}"].(map[string]interface{})["get"].(map[string]interface{})
	got := readOp["x-flexera-tf-volatile"]
	want := []interface{}{"description"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("x-flexera-tf-volatile on read op = %v, want %v", got, want)
	}
	createOp := paths["/policy/v1/orgs/{orgId}/projects/{projectId}/applied-policies"].(map[string]interface{})["post"].(map[string]interface{})
	if v, present := createOp["x-flexera-tf-volatile"]; present {
		t.Errorf("x-flexera-tf-volatile should NOT be on create op (read-time concept), got %v", v)
	}
}

func TestApplyTFOverrides_VolatileRejectsUnknownProperty(t *testing.T) {
	doc := fixtureSpec()
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"applied-policy": {Volatile: []string{"not_a_real_property"}},
		},
	}
	err := applyTFOverrides(doc, overrides)
	if err == nil || !strings.Contains(err.Error(), "no such property") {
		t.Fatalf("want no-such-property error, got %v", err)
	}
}

func TestApplyTFOverrides_VolatileRejectsDuplicates(t *testing.T) {
	// Use fixtureSpecWithSchemas so the resource actually has the
	// referenced property; otherwise the no-such-property branch fires
	// first and masks the duplicate-detection branch we want to cover.
	doc := fixtureSpecWithSchemas([]string{"name"})
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"applied-policy": {Volatile: []string{"name", "name"}},
		},
	}
	err := applyTFOverrides(doc, overrides)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("want duplicate error, got %v", err)
	}
}

// TestLoadTFOverrides_AcceptsVolatileKey verifies the loader passes
// `volatile:` through known-fields strict decoding.
func TestLoadTFOverrides_AcceptsVolatileKey(t *testing.T) {
	body := `version: 1
resources:
  applied-policy:
    volatile:
      - description
`
	dir := writeOverrides(t, body)
	got, err := loadTFOverrides(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	entry := got.Resources["applied-policy"]
	if !reflect.DeepEqual(entry.Volatile, []string{"description"}) {
		t.Errorf("volatile = %v, want [description]", entry.Volatile)
	}
}

// fixtureSpecSplitLifecycle returns a synthetic spec mirroring AWS
// bill-connect's split-lifecycle shape: distinct x-flexera-resource
// labels for create/update vs read/delete, sharing one {id}.
func fixtureSpecSplitLifecycle() map[string]interface{} {
	mkOp := func(opID, res, action string) map[string]interface{} {
		return map[string]interface{}{
			"operationId":        opID,
			"x-flexera-resource": res,
			"x-flexera-action":   action,
		}
	}
	return map[string]interface{}{
		"paths": map[string]interface{}{
			"/x/iam-role": map[string]interface{}{
				"post": mkOp("X_createIAMRole", "x-iam-role", "create"),
			},
			"/x/iam-role/{id}": map[string]interface{}{
				"patch": mkOp("X_updateIAMRole", "x-iam-role", "update"),
			},
			"/x/iam-user": map[string]interface{}{
				"post": mkOp("X_createIAMUser", "x-iam-user", "create"),
			},
			"/x/iam-user/{id}": map[string]interface{}{
				"patch": mkOp("X_updateIAMUser", "x-iam-user", "update"),
			},
			"/x/{id}": map[string]interface{}{
				"get":    mkOp("X_show", "x-shared", "get"),
				"delete": mkOp("X_delete", "x-shared", "delete"),
			},
		},
	}
}

// TestApplyTFOverrides_CrudCrossLabelStampsAliases verifies that when a
// `crud:` override binds opIds whose underlying x-flexera-resource
// label differs from the entry's resource name, the borrowed ops get
// the entry's resource name appended to x-flexera-tf-resource-aliases.
func TestApplyTFOverrides_CrudCrossLabelStampsAliases(t *testing.T) {
	doc := fixtureSpecSplitLifecycle()
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"x-iam-role": {
				CRUD: &tfCRUDBinding{
					Create: "X_createIAMRole",
					Read:   "X_show",
					Update: "X_updateIAMRole",
					Delete: "X_delete",
				},
			},
		},
	}
	if err := applyTFOverrides(doc, overrides); err != nil {
		t.Fatalf("apply: %v", err)
	}
	paths := doc["paths"].(map[string]interface{})
	showOp := paths["/x/{id}"].(map[string]interface{})["get"].(map[string]interface{})
	deleteOp := paths["/x/{id}"].(map[string]interface{})["delete"].(map[string]interface{})

	expectAlias := func(op map[string]interface{}, label string, want []string) {
		t.Helper()
		got, _ := op["x-flexera-tf-resource-aliases"].([]interface{})
		gotStr := []string{}
		for _, v := range got {
			if s, ok := v.(string); ok {
				gotStr = append(gotStr, s)
			}
		}
		if !reflect.DeepEqual(gotStr, want) {
			t.Errorf("%s aliases = %v, want %v", label, gotStr, want)
		}
	}
	expectAlias(showOp, "show", []string{"x-iam-role"})
	expectAlias(deleteOp, "delete", []string{"x-iam-role"})

	// The native x-flexera-resource label must be preserved.
	if got := showOp["x-flexera-resource"]; got != "x-shared" {
		t.Errorf("show op x-flexera-resource = %v, want x-shared (native preserved)", got)
	}
}

// TestApplyTFOverrides_CrudCrossLabelManyToOne verifies many-to-one
// safety: two distinct override entries can both alias the same op,
// and BOTH alias entries appear on the borrowed op.
func TestApplyTFOverrides_CrudCrossLabelManyToOne(t *testing.T) {
	doc := fixtureSpecSplitLifecycle()
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"x-iam-role": {
				CRUD: &tfCRUDBinding{
					Create: "X_createIAMRole",
					Read:   "X_show",
					Update: "X_updateIAMRole",
					Delete: "X_delete",
				},
			},
			"x-iam-user": {
				CRUD: &tfCRUDBinding{
					Create: "X_createIAMUser",
					Read:   "X_show",
					Update: "X_updateIAMUser",
					Delete: "X_delete",
				},
			},
		},
	}
	if err := applyTFOverrides(doc, overrides); err != nil {
		t.Fatalf("apply: %v", err)
	}
	showOp := doc["paths"].(map[string]interface{})["/x/{id}"].(map[string]interface{})["get"].(map[string]interface{})
	got, _ := showOp["x-flexera-tf-resource-aliases"].([]interface{})
	gotSet := map[string]bool{}
	for _, v := range got {
		if s, ok := v.(string); ok {
			gotSet[s] = true
		}
	}
	if !gotSet["x-iam-role"] || !gotSet["x-iam-user"] {
		t.Errorf("show op aliases = %v, want both x-iam-role and x-iam-user present", got)
	}
}

// TestApplyTFOverrides_VirtualResourceFromCrud verifies a resource
// override entry can be "virtual" (no native x-flexera-resource match)
// when CRUD operationIds are explicitly pinned. The bound ops must get
// alias + CRUD-binding stamps for downstream generators.
func TestApplyTFOverrides_VirtualResourceFromCrud(t *testing.T) {
	doc := fixtureSpecSplitLifecycle()
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"virtual-iam-role": {
				CRUD: &tfCRUDBinding{
					Create: "X_createIAMRole",
					Read:   "X_show",
					Update: "X_updateIAMRole",
					Delete: "X_delete",
				},
				IDField:  "id",
				ImportID: "{id}",
			},
		},
	}
	if err := applyTFOverrides(doc, overrides); err != nil {
		t.Fatalf("apply: %v", err)
	}
	paths := doc["paths"].(map[string]interface{})
	showOp := paths["/x/{id}"].(map[string]interface{})["get"].(map[string]interface{})
	aliases, _ := showOp["x-flexera-tf-resource-aliases"].([]interface{})
	if len(aliases) == 0 {
		t.Fatalf("expected alias stamp on borrowed read op")
	}
	found := false
	for _, v := range aliases {
		if s, _ := v.(string); s == "virtual-iam-role" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected virtual-iam-role in aliases, got %v", aliases)
	}
	binding, _ := showOp["x-flexera-tf-crud-binding"].(map[string]interface{})
	if binding["read"] != "X_show" {
		t.Fatalf("crud binding not stamped correctly on borrowed read op: %v", binding)
	}
}

// TestApplyTFOverrides_CrudSameLabelNoAliases verifies that when a
// `crud:` override pins opIds that DO carry the entry's resource label
// natively, no x-flexera-tf-resource-aliases is stamped.
func TestApplyTFOverrides_CrudSameLabelNoAliases(t *testing.T) {
	doc := fixtureSpec()
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"applied-policy": {
				CRUD: &tfCRUDBinding{
					Create: "Policy_Applied_Policy_create",
					Read:   "Policy_Applied_Policy_show",
					Update: "Policy_Applied_Policy_update",
					Delete: "Policy_Applied_Policy_delete",
				},
			},
		},
	}
	if err := applyTFOverrides(doc, overrides); err != nil {
		t.Fatalf("apply: %v", err)
	}
	paths := doc["paths"].(map[string]interface{})
	for path, item := range paths {
		for method, opAny := range item.(map[string]interface{}) {
			op := opAny.(map[string]interface{})
			if _, present := op["x-flexera-tf-resource-aliases"]; present {
				t.Errorf("%s %s should NOT have x-flexera-tf-resource-aliases (same-label crud), got %v",
					method, path, op["x-flexera-tf-resource-aliases"])
			}
		}
	}
}

// TestApplyTFOverrides_WrapEnvelopeFalseStampsExtension verifies that
// `wrap_envelope: false` stamps x-flexera-tf-wrap-envelope-disabled=true
// on the read op, force-disabling the generator's auto-detection.
func TestApplyTFOverrides_WrapEnvelopeFalseStampsExtension(t *testing.T) {
	doc := fixtureSpecWithSchemas([]string{"name"})
	falseVal := false
	overrides := &tfOverridesFile{
		Version: 1,
		Resources: map[string]tfResourceOverride{
			"applied-policy": {WrapEnvelope: &falseVal},
		},
	}
	if err := applyTFOverrides(doc, overrides); err != nil {
		t.Fatalf("apply: %v", err)
	}
	paths := doc["paths"].(map[string]interface{})
	readOp := paths["/policy/v1/orgs/{orgId}/projects/{projectId}/applied-policies/{appliedPolicyId}"].(map[string]interface{})["get"].(map[string]interface{})
	got, present := readOp["x-flexera-tf-wrap-envelope-disabled"]
	if !present {
		t.Fatalf("x-flexera-tf-wrap-envelope-disabled missing on read op")
	}
	if v, _ := got.(bool); !v {
		t.Errorf("x-flexera-tf-wrap-envelope-disabled = %v, want true", got)
	}
}

// TestApplyTFOverrides_WrapEnvelopeNilNoStamp verifies that the default
// (no override) leaves auto-detection enabled (no extension stamped).
func TestApplyTFOverrides_WrapEnvelopeNilNoStamp(t *testing.T) {
	doc := fixtureSpecWithSchemas([]string{"name"})
	overrides := &tfOverridesFile{
		Version:   1,
		Resources: map[string]tfResourceOverride{"applied-policy": {}},
	}
	if err := applyTFOverrides(doc, overrides); err != nil {
		t.Fatalf("apply: %v", err)
	}
	paths := doc["paths"].(map[string]interface{})
	readOp := paths["/policy/v1/orgs/{orgId}/projects/{projectId}/applied-policies/{appliedPolicyId}"].(map[string]interface{})["get"].(map[string]interface{})
	if _, present := readOp["x-flexera-tf-wrap-envelope-disabled"]; present {
		t.Errorf("x-flexera-tf-wrap-envelope-disabled stamped on default-config read op (should be absent)")
	}
}

// TestLoadTFOverrides_AcceptsWrapEnvelopeKey verifies the loader passes
// `wrap_envelope:` through known-fields strict decoding.
func TestLoadTFOverrides_AcceptsWrapEnvelopeKey(t *testing.T) {
	body := `version: 1
resources:
  applied-policy:
    wrap_envelope: false
`
	dir := writeOverrides(t, body)
	got, err := loadTFOverrides(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	entry := got.Resources["applied-policy"]
	if entry.WrapEnvelope == nil {
		t.Fatalf("wrap_envelope = nil, want explicit false")
	}
	if *entry.WrapEnvelope {
		t.Errorf("wrap_envelope = true, want false")
	}
}
