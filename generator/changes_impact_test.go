package main

import (
	"strings"
	"testing"
)

func TestVersionImpact(t *testing.T) {
	tests := []struct{ name, before, after, impact string }{
		{"no changes", `{"openapi":"3.0.0"}`, `{"openapi":"3.0.0"}`, "none"},
		{"formatting", `{"openapi":"3.0.0"}`, `{ "openapi": "3.0.0" }`, "none"},
		{"version", `{"openapi":"3.0.0","info":{"version":"1"}}`, `{"openapi":"3.0.0","info":{"version":"2"}}`, "patch"},
		{"description", `{"openapi":"3.0.0","components":{"schemas":{"A":{"description":"old"}}}}`, `{"openapi":"3.0.0","components":{"schemas":{"A":{"description":"new"}}}}`, "patch"},
		{"description removal", `{"openapi":"3.0.0","components":{"schemas":{"A":{"description":"old"}}}}`, `{"openapi":"3.0.0","components":{"schemas":{"A":{}}}}`, "patch"},
		{"examples", `{"openapi":"3.0.0","components":{"schemas":{"A":{"example":1}}}}`, `{"openapi":"3.0.0","components":{"schemas":{"A":{"example":2}}}}`, "patch"},
		{"path addition", `{"openapi":"3.0.0","paths":{}}`, `{"openapi":"3.0.0","paths":{"/new":{}}}`, "minor"},
		{"path removal", `{"openapi":"3.0.0","paths":{"/old":{}}}`, `{"openapi":"3.0.0","paths":{}}`, "major"},
		{"property addition", `{"openapi":"3.0.0","components":{"schemas":{"A":{"properties":{}}}}}`, `{"openapi":"3.0.0","components":{"schemas":{"A":{"properties":{"new":{"type":"string"}}}}}}`, "minor"},
		{"property named description", `{"openapi":"3.0.0","components":{"schemas":{"A":{"properties":{"description":{"type":"string"}}}}}}`, `{"openapi":"3.0.0","components":{"schemas":{"A":{"properties":{}}}}}`, "major"},
		{"extension description", `{"openapi":"3.0.0","x-custom":{"description":"old"}}`, `{"openapi":"3.0.0","x-custom":{"description":"new"}}`, "major"},
		{"type change", `{"openapi":"3.0.0","components":{"schemas":{"A":{"type":"string"}}}}`, `{"openapi":"3.0.0","components":{"schemas":{"A":{"type":"integer"}}}}`, "major"},
		{"new required list", `{"openapi":"3.0.0","components":{"schemas":{"A":{}}}}`, `{"openapi":"3.0.0","components":{"schemas":{"A":{"required":["x"]}}}}`, "major"},
		{"requirement replacement", `{"openapi":"3.0.0","components":{"schemas":{"A":{"required":["x"]}}}}`, `{"openapi":"3.0.0","components":{"schemas":{"A":{"required":["y"]}}}}`, "major"},
		{"relaxed requirement", `{"openapi":"3.0.0","components":{"schemas":{"A":{"required":["x","y"]}}}}`, `{"openapi":"3.0.0","components":{"schemas":{"A":{"required":["x"]}}}}`, "minor"},
		{"enum narrowing", `{"openapi":"3.0.0","components":{"schemas":{"A":{"enum":["x","y"]}}}}`, `{"openapi":"3.0.0","components":{"schemas":{"A":{"enum":["x"]}}}}`, "major"},
		{"enum expansion", `{"openapi":"3.0.0","components":{"schemas":{"A":{"enum":["x"]}}}}`, `{"openapi":"3.0.0","components":{"schemas":{"A":{"enum":["x","y"]}}}}`, "minor"},
		{"new constraint", `{"openapi":"3.0.0","components":{"schemas":{"A":{}}}}`, `{"openapi":"3.0.0","components":{"schemas":{"A":{"maxLength":10}}}}`, "major"},
		{"new security", `{"openapi":"3.0.0"}`, `{"openapi":"3.0.0","security":[{"auth":[]}]}`, "major"},
		{"new required body", `{"openapi":"3.0.0","paths":{"/a":{"post":{}}}}`, `{"openapi":"3.0.0","paths":{"/a":{"post":{"requestBody":{"required":true}}}}}`, "major"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := compareSpecBytes("test.json", []byte(tt.before), []byte(tt.after))
			if err != nil {
				t.Fatal(err)
			}
			if file.VersionImpact != tt.impact {
				t.Fatalf("got %+v, want %s", file, tt.impact)
			}
			report := ChangeReport{Files: []FileChange{file}}
			if got := reportVersionImpact(report); got != tt.impact {
				t.Fatalf("report impact %s", got)
			}
		})
	}
	if got := reportVersionImpact(ChangeReport{Files: []FileChange{
		{Status: "examples-only"}, {Changes: []ChangeRecord{{VersionImpact: "minor"}}}, {Changes: []ChangeRecord{{VersionImpact: "major"}}},
	}}); got != "major" {
		t.Fatal(got)
	}
}

func TestDescriptionConsolidation(t *testing.T) {
	before := []byte(`{"openapi":"3.0.0","info":{"description":"old"},"paths":{"/a":{"get":{"description":"old"}}}}`)
	after := []byte(`{"openapi":"3.0.0","info":{"description":"new"},"paths":{"/a":{"get":{"description":"new"}}}}`)
	file, err := compareSpecBytes("openapi3.json", before, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Changes) != 2 || file.Status != "functional" {
		t.Fatal(file)
	}
	for _, markdown := range []bool{false, true} {
		text := renderChangeReport(ChangeReport{HasFunctionalChanges: true, Files: []FileChange{file}}, markdown)
		if !strings.Contains(text, "2 description changes (nonbreaking; consolidated)") || strings.Contains(text, "modified") || !strings.Contains(text, "version bump: patch") {
			t.Fatal(text)
		}
	}
}

func TestReportCodeSpan(t *testing.T) {
	label := reportLabel("/a~~b` <script>\n")
	if !strings.HasPrefix(label, "`` ") || !strings.HasSuffix(label, " ``") || strings.Contains(label, "\n") {
		t.Fatal(label)
	}
	if got := humanChangePath("/paths/~1v1~1users/get/responses/200/content/application~1json"); got != "paths → /v1/users → get → responses → 200 → content → application/json" {
		t.Fatal(got)
	}
	if got := humanChangePath("/a~01~1b"); got != "a~1/b" {
		t.Fatal(got)
	}
}

func TestCompanionReportDeduplication(t *testing.T) {
	before := []byte(`{"openapi":"3.0.0","info":{"description":"old"}}`)
	after := []byte(`{"openapi":"3.0.0","info":{"description":"new"}}`)
	a, err := compareSpecBytes("openapi3.json", before, after)
	if err != nil {
		t.Fatal(err)
	}
	b, err := compareSpecBytes("openapi3.yaml", before, after)
	if err != nil {
		t.Fatal(err)
	}
	report := ChangeReport{HasFunctionalChanges: true, Files: []FileChange{a, b}}
	if text := renderChangeReport(report, true); strings.Contains(text, "openapi3.yaml") {
		t.Fatal(text)
	}
	b, err = compareSpecBytes("openapi3.yaml", before, []byte(`{"openapi":"3.0.0","info":{"description":"different"}}`))
	if err != nil {
		t.Fatal(err)
	}
	report.Files[1] = b
	if text := renderChangeReport(report, true); !strings.Contains(text, "openapi3.yaml") {
		t.Fatal(text)
	}
}
