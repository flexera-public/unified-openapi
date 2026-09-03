package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSpecsDir_Default(t *testing.T) {
	// No flags, no env var
	os.Unsetenv("OPENAPI_SPECS_DIR")

	dir, args, err := resolveSpecsDir([]string{"list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "." {
		t.Errorf("expected default '.', got %s", dir)
	}
	if len(args) != 1 || args[0] != "list" {
		t.Errorf("expected args [list], got %v", args)
	}
}

func TestResolveSpecsDir_EnvVar(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	os.Setenv("OPENAPI_SPECS_DIR", tmpDir)
	defer os.Unsetenv("OPENAPI_SPECS_DIR")

	dir, args, err := resolveSpecsDir([]string{"fetch", "all"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != tmpDir {
		t.Errorf("expected env var dir, got %s", dir)
	}
	if len(args) != 2 {
		t.Errorf("expected 2 args, got %v", args)
	}
}

func TestResolveSpecsDir_FlagOverridesEnv(t *testing.T) {
	// Both env var and flag set; flag wins
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()
	os.Setenv("OPENAPI_SPECS_DIR", tmpDir1)
	defer os.Unsetenv("OPENAPI_SPECS_DIR")

	dir, args, err := resolveSpecsDir([]string{"--openapi_specs_dir", tmpDir2, "list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != tmpDir2 {
		t.Errorf("expected flag dir, got %s", dir)
	}
	if len(args) != 1 || args[0] != "list" {
		t.Errorf("expected args [list], got %v", args)
	}
}

func TestResolveSpecsDir_EqualsSign(t *testing.T) {
	tmpDir := t.TempDir()
	os.Unsetenv("OPENAPI_SPECS_DIR")

	dir, args, err := resolveSpecsDir([]string{"--openapi_specs_dir=" + tmpDir, "validate", "all"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != tmpDir {
		t.Errorf("expected equals-sign dir, got %s", dir)
	}
	if len(args) != 2 {
		t.Errorf("expected 2 args, got %v", args)
	}
}

func TestResolveSpecsDir_MissingValue(t *testing.T) {
	os.Unsetenv("OPENAPI_SPECS_DIR")

	_, _, err := resolveSpecsDir([]string{"--openapi_specs_dir"})
	if err == nil {
		t.Error("expected error for missing flag value")
	}
}

func TestResolveSpecsDir_NonExistentDirectory(t *testing.T) {
	os.Unsetenv("OPENAPI_SPECS_DIR")

	_, _, err := resolveSpecsDir([]string{"--openapi_specs_dir", "/nonexistent/path/xyz", "list"})
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestResolveSpecsDir_NotADirectory(t *testing.T) {
	// Create a temp file (not directory)
	tmpFile := t.TempDir() + "/somefile.txt"
	if f, err := os.Create(tmpFile); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	} else {
		f.Close()
	}

	os.Unsetenv("OPENAPI_SPECS_DIR")

	_, _, err := resolveSpecsDir([]string{"--openapi_specs_dir", tmpFile, "list"})
	if err == nil {
		t.Error("expected error when path is not a directory")
	}
}

func TestResolveSpecsDir_MultipleFlags(t *testing.T) {
	// Last flag wins
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()
	os.Unsetenv("OPENAPI_SPECS_DIR")

	dir, args, err := resolveSpecsDir([]string{"--openapi_specs_dir", tmpDir1, "--openapi_specs_dir", tmpDir2, "list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != tmpDir2 {
		t.Errorf("expected last flag value, got %s", dir)
	}
	if len(args) != 1 {
		t.Errorf("expected 1 arg after flag removal, got %v", args)
	}
}

func TestResolveSpecsDir_TrimWhitespace(t *testing.T) {
	tmpDir := t.TempDir()
	os.Unsetenv("OPENAPI_SPECS_DIR")

	dir, _, err := resolveSpecsDir([]string{"--openapi_specs_dir=" + tmpDir + " ", "list"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != tmpDir {
		t.Errorf("expected trimmed path, got %s", dir)
	}
}

func TestSedReplace_RemovesDanglingUnionArm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openapi.json")
	const brokenArm = `          {
            "$ref": "#/components/schemas/AzureCostAndUsageUpdateModel"
          },`
	input := `{
  "anyOf": [
    {"$ref":"#/components/schemas/AwsCostAndUsageModel"},
` + brokenArm + `
    {"$ref":"#/components/schemas/AzureCostAndUsageModel"}
  ]
}`
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := sedReplace(path, brokenArm, ""); err != nil {
		t.Fatalf("sedReplace() error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "AzureCostAndUsageUpdateModel") {
		t.Fatal("dangling update-model ref was not removed")
	}
	for _, want := range []string{"AwsCostAndUsageModel", "AzureCostAndUsageModel"} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("valid neighboring ref %q was removed", want)
		}
	}
	var parsed interface{}
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("processed document is invalid JSON: %v", err)
	}
}

func TestSedReplace_MissingPatternIsActionable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openapi.json")
	if err := os.WriteFile(path, []byte(`{"openapi":"3.0.3"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := sedReplace(path, "missing upstream fragment", "")
	if err == nil {
		t.Fatal("sedReplace() succeeded with a missing pattern")
	}
	for _, want := range []string{"pattern not found", "upstream document may have changed"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("sedReplace() error %q does not contain %q", err, want)
		}
	}
}

func TestRemoveLocalRef_RemovesUnionArmIndependentOfFormatting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openapi.json")
	input := `{"anyOf":[{"$ref":"#/components/schemas/Aws"}, { "$ref" : "#/components/schemas/Missing" },{"$ref":"#/components/schemas/Azure"}]}`
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := removeLocalRef(path, "#/components/schemas/Missing"); err != nil {
		t.Fatalf("removeLocalRef() error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "Missing") {
		t.Fatal("dangling ref was not removed")
	}
	for _, want := range []string{"#/components/schemas/Aws", "#/components/schemas/Azure"} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("valid neighboring ref %q was removed", want)
		}
	}
}

func TestRemoveLocalRef_MissingRefIsActionable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openapi.json")
	if err := os.WriteFile(path, []byte(`{"anyOf":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := removeLocalRef(path, "#/components/schemas/Missing")
	if err == nil || !strings.Contains(err.Error(), "upstream document may have changed") {
		t.Fatalf("removeLocalRef() error = %v, want upstream-change guidance", err)
	}
}

func TestSetOperationID_TargetsPathAndMethod(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openapi.json")
	input := `{"paths":{"/exports":{"get":{"operationId":"Export "}},"/exports/{id}":{"get":{"operationId":"Export "}}}}`
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := setOperationID(path, "/exports/{id}", "GET", "ExportDownload"); err != nil {
		t.Fatalf("setOperationID() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	paths := doc["paths"].(map[string]interface{})
	if got := paths["/exports"].(map[string]interface{})["get"].(map[string]interface{})["operationId"]; got != "Export " {
		t.Fatalf("unrelated operationId = %q", got)
	}
	if got := paths["/exports/{id}"].(map[string]interface{})["get"].(map[string]interface{})["operationId"]; got != "ExportDownload" {
		t.Fatalf("target operationId = %q", got)
	}
}

func TestSetOperationID_MissingPathIsActionable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openapi.json")
	if err := os.WriteFile(path, []byte(`{"paths":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := setOperationID(path, "/missing", "get", "Missing")
	if err == nil || !strings.Contains(err.Error(), "upstream document may have changed") {
		t.Fatalf("setOperationID() error = %v, want upstream-change guidance", err)
	}
}
