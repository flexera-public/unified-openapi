package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSwaggerMetadataFidelity(t *testing.T) {
	var source map[string]interface{}
	if err := json.Unmarshal([]byte(`{
		"swagger":"2.0",
		"info":{"title":"Example","version":"1"},
		"paths":{"/widgets":{"post":{
			"operationId":"create","summary":"Create a widget","description":"Operation guidance",
			"deprecated":true,"externalDocs":{"url":"https://example.invalid/docs"},
			"parameters":[
				{"name":"body","in":"body","required":true,"description":"Body guidance","schema":{"type":"object"}},
				{"name":"names","in":"query","description":"Names to select","type":"array","items":{"type":"string"},"minItems":1,"maxItems":4,"uniqueItems":true,"collectionFormat":"csv"},
				{"name":"count","in":"query","type":"integer","minimum":0,"exclusiveMinimum":true,"maximum":20,"exclusiveMaximum":true,"multipleOf":2,"default":2}
			],
			"responses":{"200":{"description":"Created","schema":{"type":"object"},
				"examples":{"application/json":{"name":"sample"}},
				"headers":{"Count":{"description":"Count guidance","type":"integer","minimum":0,"exclusiveMinimum":true,"multipleOf":2}}
			}}
		}}}
	}`), &source); err != nil {
		t.Fatal(err)
	}
	doc := convertSwagger2ToOpenAPI3(source)
	op := doc["paths"].(map[string]interface{})["/widgets"].(map[string]interface{})["post"].(map[string]interface{})
	for key, want := range map[string]interface{}{
		"description": "Operation guidance", "deprecated": true,
		"externalDocs": map[string]interface{}{"url": "https://example.invalid/docs"},
	} {
		if !reflect.DeepEqual(op[key], want) {
			t.Fatalf("%s = %#v, want %#v", key, op[key], want)
		}
	}
	body := op["requestBody"].(map[string]interface{})
	if body["description"] != "Body guidance" || body["required"] != true {
		t.Fatalf("body metadata lost: %#v", body)
	}
	params := op["parameters"].([]interface{})
	names := params[0].(map[string]interface{})
	schema := names["schema"].(map[string]interface{})
	if names["description"] != "Names to select" || names["explode"] != false || schema["minItems"] != float64(1) || schema["maxItems"] != float64(4) || schema["uniqueItems"] != true {
		t.Fatalf("array metadata lost: %#v", names)
	}
	count := params[1].(map[string]interface{})["schema"].(map[string]interface{})
	for _, key := range []string{"exclusiveMinimum", "exclusiveMaximum", "multipleOf", "default"} {
		if _, exists := count[key]; !exists {
			t.Fatalf("numeric constraint %s lost", key)
		}
	}
	response := op["responses"].(map[string]interface{})["200"].(map[string]interface{})
	media := response["content"].(map[string]interface{})["application/json"].(map[string]interface{})
	if !reflect.DeepEqual(media["example"], map[string]interface{}{"name": "sample"}) {
		t.Fatalf("response example lost: %#v", media)
	}
	header := response["headers"].(map[string]interface{})["Count"].(map[string]interface{})
	if header["description"] != "Count guidance" || header["schema"].(map[string]interface{})["multipleOf"] != float64(2) {
		t.Fatalf("header metadata lost: %#v", header)
	}
}

func TestMergePreservesMetadata(t *testing.T) {
	doc := map[string]interface{}{
		"paths": map[string]interface{}{"/widgets": map[string]interface{}{
			"get": map[string]interface{}{
				"operationId": "get", "description": "Guidance", "deprecated": true,
				"parameters": []interface{}{map[string]interface{}{
					"name": "filter", "in": "query", "examples": map[string]interface{}{
						"named": map[string]interface{}{"summary": "A named example", "value": "name eq 'sample'"},
					},
				}},
			},
		}},
	}
	normalized, err := normalizeServiceDocument(doc, SpecConfig{Service: "example", Name: "Example"})
	if err != nil {
		t.Fatal(err)
	}
	paths, components, tags := map[string]interface{}{}, map[string]interface{}{}, map[string]map[string]interface{}{}
	if err := mergeDocumentIntoUnified(normalized, paths, components, tags); err != nil {
		t.Fatal(err)
	}
	op := paths["/widgets"].(map[string]interface{})["get"].(map[string]interface{})
	if op["description"] != "Guidance" || op["deprecated"] != true {
		t.Fatalf("operation metadata lost: %#v", op)
	}
	param := op["parameters"].([]interface{})[0].(map[string]interface{})
	if !reflect.DeepEqual(param, doc["paths"].(map[string]interface{})["/widgets"].(map[string]interface{})["get"].(map[string]interface{})["parameters"].([]interface{})[0]) {
		t.Fatalf("parameter metadata changed: %#v", param)
	}
}
