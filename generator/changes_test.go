package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCompareSpecBytes(t *testing.T) {
	tests := []struct{ name, before, after, status string }{
		{"unchanged", `{"openapi":"3.0.0"}`, `{"openapi":"3.0.0"}`, "unchanged"},
		{"format", `{"openapi":"3.0.0","info":{"title":"a"}}`, `{ "info": {"title":"a"}, "openapi":"3.0.0" }`, "formatting-only"},
		{"schema example", `{"openapi":"3.0.0","components":{"schemas":{"A":{"type":"string","example":"old"}}}}`, `{"openapi":"3.0.0","components":{"schemas":{"A":{"type":"string","example":"new"}}}}`, "examples-only"},
		{"schema example addition", `{"openapi":"3.1.0","components":{"schemas":{"A":{"type":"string"}}}}`, `{"openapi":"3.1.0","components":{"schemas":{"A":{"type":"string","examples":["secret"]}}}}`, "examples-only"},
		{"named examples", `{"openapi":"3.0.0"}`, `{"openapi":"3.0.0","components":{"examples":{"secret":{"summary":"secret","externalValue":"secret","$ref":"secret"}}}}`, "examples-only"},
		{"actual property", `{"openapi":"3.0.0","components":{"schemas":{"A":{"properties":{"examples":{"type":"string"}}}}}}`, `{"openapi":"3.0.0","components":{"schemas":{"A":{"properties":{"examples":{"type":"integer"}}}}}}`, "functional"},
		{"component name", `{"openapi":"3.0.0","components":{"schemas":{"examples":{"type":"string"}}}}`, `{"openapi":"3.0.0","components":{"schemas":{"examples":{"type":"number"}}}}`, "functional"},
		{"opaque extension", `{"openapi":"3.0.0","x-custom":{"examples":1}}`, `{"openapi":"3.0.0","x-custom":{"examples":2}}`, "functional"},
		{"default payload", `{"openapi":"3.0.0","components":{"schemas":{"A":{"default":{"example":1}}}}}`, `{"openapi":"3.0.0","components":{"schemas":{"A":{"default":{"example":2}}}}}`, "functional"},
		{"large integer", `{"openapi":"3.0.0","x-value":9007199254740992}`, `{"openapi":"3.0.0","x-value":9007199254740993}`, "functional"},
		{"numeric spellings", `{"openapi":"3.0.0","x-value":1}`, `{"openapi":"3.0.0","x-value":1.0e0}`, "formatting-only"},
		{"null missing", `{"openapi":"3.0.0"}`, `{"openapi":"3.0.0","x-value":null}`, "functional"},
		{"array order", `{"openapi":"3.0.0","x-value":[1,2]}`, `{"openapi":"3.0.0","x-value":[2,1]}`, "functional"},
		{"empty components", `{"openapi":"3.0.0"}`, `{"openapi":"3.0.0","components":{}}`, "functional"},
		{"swagger examples", `{"swagger":"2.0","paths":{"/a":{"get":{"responses":{"200":{"description":"ok","examples":{"application/json":{"secret":1}}}}}}}}`, `{"swagger":"2.0","paths":{"/a":{"get":{"responses":{"200":{"description":"ok","examples":{"application/json":{"secret":2}}}}}}}}`, "examples-only"},
		{"media example", `{"openapi":"3.0.0","paths":{"/a":{"get":{"responses":{"200":{"content":{"application/json":{"example":1}}}}}}}}`, `{"openapi":"3.0.0","paths":{"/a":{"get":{"responses":{"200":{"content":{"application/json":{"example":2}}}}}}}}`, "examples-only"},
		{"parameter example", `{"openapi":"3.0.0","paths":{"/a":{"get":{"parameters":[{"name":"a","in":"query","examples":{"a":{"value":1}}}]}}}}`, `{"openapi":"3.0.0","paths":{"/a":{"get":{"parameters":[{"name":"a","in":"query","examples":{"a":{"value":2}}}]}}}}`, "examples-only"},
		{"required field", `{"openapi":"3.0.0","components":{"schemas":{"A":{"required":[]}}}}`, `{"openapi":"3.0.0","components":{"schemas":{"A":{"required":["examples"]}}}}`, "functional"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := compareSpecBytes("test.json", []byte(tt.before), []byte(tt.after))
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tt.status {
				t.Fatalf("got %+v, want %s", got, tt.status)
			}
			reverse, err := compareSpecBytes("test.json", []byte(tt.after), []byte(tt.before))
			if err != nil || reverse.Status != tt.status {
				t.Fatalf("reverse: %+v %v", reverse, err)
			}
		})
	}
}

func TestChangeParseErrors(t *testing.T) {
	for _, data := range []string{`{"openapi":"3.0.0","a":1,"a":2}`, `{"openapi":"3.0.0",}`, "openapi: 3.0.0\na: 1\na: 2\n", "openapi: 3.0.0\n---\na: 2\n", "openapi: 3.0.0\na: &x [1]\nb: *x\n", "openapi: 3.0.0\na: .nan\n", `{"hello":"world"}`} {
		name := "test.yaml"
		if strings.HasPrefix(data, "{") {
			name = "test.json"
		}
		if _, err := parseChangeDocument(name, []byte(data)); err == nil {
			t.Errorf("accepted invalid document %s", data)
		}
	}
}
func TestChangeYAMLNumbers(t *testing.T) {
	a, err := parseChangeDocument("a.json", []byte(`{"openapi":"3.0.0","x-number":9007199254740993}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := parseChangeDocument("a.yaml", []byte("openapi: 3.0.0\nx-number: 9007199254740993\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("JSON/YAML mismatch: %v %v", a, b)
	}
}
func TestFunctionalReportNoExampleLeaks(t *testing.T) {
	before := []byte(`{"openapi":"3.0.0","components":{"schemas":{"A":{"type":"string","example":"old"}}}}`)
	after := []byte(`{"openapi":"3.0.0","components":{"schemas":{"A":{"type":"number","example":"SENTINEL_VALUE"}},"examples":{"SENTINEL_NAME":{"value":"secret"}}},"paths":{"/new":{"get":{"responses":{"200":{"description":"ok","content":{"application/json":{"examples":{"SENTINEL_NESTED":{"value":"secret"}}}}}}}}}}`)
	file, err := compareSpecBytes("openapi3.json", before, after)
	if err != nil {
		t.Fatal(err)
	}
	report := ChangeReport{true, []FileChange{file, {File: "SENTINEL_FILE.json", Status: "examples-only"}}}
	for _, markdown := range []bool{false, true} {
		text := renderChangeReport(report, markdown)
		if strings.Contains(text, "SENTINEL") || strings.Contains(text, "example") {
			t.Fatalf("example leak: %s", text)
		}
		if !strings.Contains(text, "/components/schemas/A/type") {
			t.Fatalf("missing change: %s", text)
		}
	}
	raw, _ := parseChangeDocument("test.json", after)
	copyBefore, _ := json.Marshal(raw)
	withoutExamples(raw, "root")
	copyAfter, _ := json.Marshal(raw)
	if string(copyBefore) != string(copyAfter) {
		t.Fatal("filter mutated input")
	}
	for _, nilBefore := range []bool{true, false} {
		b, a := before, []byte(nil)
		if nilBefore {
			b, a = nil, after
		}
		f, err := compareSpecBytes("added.json", b, a)
		if err != nil || f.Status != "functional" {
			t.Fatalf("added/deleted: %+v %v", f, err)
		}
	}
}
func TestChangePointersAndLimit(t *testing.T) {
	var changes []ChangeRecord
	functionalDiff(map[string]any{}, map[string]any{"a/b~c": true}, "", &changes)
	if changes[0].Path != "/a~1b~0c" {
		t.Fatal(changes)
	}
	for i := 0; i < 105; i++ {
		changes = append(changes, ChangeRecord{"/field", "modified"})
	}
	text := renderChangeReport(ChangeReport{true, []FileChange{{File: "a.json", Status: "functional", Changes: changes}}}, true)
	if !strings.Contains(text, "6 additional functional changes omitted") {
		t.Fatal(text)
	}
}
