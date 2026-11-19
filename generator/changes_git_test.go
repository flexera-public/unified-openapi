package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func changeTestGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s %v", args, data, err)
	}
}

func writeChangeFixture(t *testing.T, root, name, data string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGitChangeReport(t *testing.T) {
	root := t.TempDir()
	changeTestGit(t, root, "init", "-q")
	changeTestGit(t, root, "config", "user.name", "Test")
	changeTestGit(t, root, "config", "user.email", "test@example.invalid")
	source := "generator/sources/test/openapi.json"
	doc := `{"openapi":"3.0.0","components":{"schemas":{"A":{"type":"string","example":1}}}}`
	writeChangeFixture(t, root, source, doc)
	writeChangeFixture(t, root, "openapi3.json", doc)
	writeChangeFixture(t, root, "openapi3.yaml", doc)
	changeTestGit(t, root, "add", ".")
	changeTestGit(t, root, "commit", "-qm", "baseline")
	clean, err := collectChangeReport(root, "HEAD", "all")
	if err != nil || clean.HasFunctionalChanges || len(clean.Files) != 0 || clean.VersionImpact != "none" {
		t.Fatalf("clean: %+v %v", clean, err)
	}
	writeChangeFixture(t, root, source, `{"openapi":"3.0.0","components":{"schemas":{"A":{"type":"string","example":2}}}}`)
	changeTestGit(t, root, "add", source)
	only, err := collectChangeReport(root, "HEAD", "all")
	if err != nil || only.HasFunctionalChanges || len(only.Files) != 1 || only.Files[0].Status != "examples-only" || only.VersionImpact != "patch" {
		t.Fatalf("examples: %+v %v", only, err)
	}
	writeChangeFixture(t, root, "openapi3.json", `{"openapi":"3.0.0","components":{"schemas":{"A":{"type":"number","example":2}}}}`)
	writeChangeFixture(t, root, "generator/sources/new/openapi.json", doc)
	writeChangeFixture(t, root, "outside.json", doc)
	if err := os.Remove(filepath.Join(root, "openapi3.yaml")); err != nil {
		t.Fatal(err)
	}
	before, err := gitChangeOutput(root, "status", "--porcelain=v1", "-z")
	if err != nil {
		t.Fatal(err)
	}
	mixed, err := collectChangeReport(root, "HEAD", "all")
	if err != nil || !mixed.HasFunctionalChanges || len(mixed.Files) != 4 {
		t.Fatalf("mixed: %+v %v", mixed, err)
	}
	sources, err := collectChangeReport(root, "HEAD", "sources")
	if err != nil || len(sources.Files) != 2 {
		t.Fatalf("sources: %+v %v", sources, err)
	}
	artifacts, err := collectChangeReport(root, "HEAD", "artifacts")
	if err != nil || len(artifacts.Files) != 2 {
		t.Fatalf("artifacts: %+v %v", artifacts, err)
	}
	if _, err := collectChangeReport(root, "missing-ref", "all"); err == nil {
		t.Fatal("accepted missing ref")
	}
	if _, err := collectChangeReport(root, "HEAD", "bad-scope"); err == nil {
		t.Fatal("accepted bad scope")
	}
	after, err := gitChangeOutput(root, "status", "--porcelain=v1", "-z")
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("comparison changed worktree/index")
	}
	writeChangeFixture(t, root, source, `not JSON`)
	if _, err := collectChangeReport(root, "HEAD", "all"); err == nil {
		t.Fatal("accepted malformed source")
	}
}

func TestChangesCommandOptions(t *testing.T) {
	for _, args := range [][]string{{"--format", "bad"}, {"--unknown"}, {"extra"}} {
		if err := runChanges(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
