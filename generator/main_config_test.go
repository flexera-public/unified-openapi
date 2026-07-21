package main

import (
	"os"
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
