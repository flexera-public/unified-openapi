package main

import "testing"

func TestApplySchemaFormatRemovals(t *testing.T) {
	doc := map[string]interface{}{"components": map[string]interface{}{"schemas": map[string]interface{}{
		"S": map[string]interface{}{"properties": map[string]interface{}{"at": map[string]interface{}{"type": "string", "format": "date-time"}}},
	}}}
	specs := []SpecConfig{{ID: "t", SchemaFormatRemovals: []SchemaFormatRemoval{{Schema: "S", Property: "at", Reason: "sentinel values"}}}}
	if err := applySchemaFormatRemovals(doc, specs); err != nil {
		t.Fatal(err)
	}
	prop := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})["S"].(map[string]interface{})["properties"].(map[string]interface{})["at"].(map[string]interface{})
	if _, has := prop["format"]; has || prop["type"] != "string" {
		t.Fatalf("prop = %v", prop)
	}
	if err := applySchemaFormatRemovals(doc, specs); err == nil {
		t.Fatal("expected stale override error")
	}
	missing := []SpecConfig{{ID: "t", SchemaFormatRemovals: []SchemaFormatRemoval{{Schema: "S", Property: "nope", Reason: "r"}}}}
	if err := applySchemaFormatRemovals(doc, missing); err == nil {
		t.Fatal("expected unmatched property error")
	}
}
