package main

import (
	"os"
	"testing"
)

func TestPrefixComponentNamesAndRefs(t *testing.T) {
	doc := map[string]interface{}{
		"paths": map[string]interface{}{
			"/foo": map[string]interface{}{
				"get": map[string]interface{}{
					"operationId": "Thing#show",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{"$ref": "#/components/schemas/Error"},
								},
							},
						},
					},
				},
			},
		},
		"components": map[string]interface{}{
			"schemas": map[string]interface{}{
				"Error": map[string]interface{}{"type": "object"},
			},
		},
	}

	renameMap := prefixComponentNames(doc, "Iam")
	rewriteRefs(doc, renameMap)
	prefixOperationIDs(doc, "Iam")

	schemas := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})
	if _, ok := schemas["Iam_Error"]; !ok {
		t.Fatalf("expected renamed component Iam_Error, got %v", schemas)
	}

	pathItem := doc["paths"].(map[string]interface{})["/foo"].(map[string]interface{})
	operation := pathItem["get"].(map[string]interface{})
	if operation["operationId"] != "Iam_Thing_show" {
		t.Fatalf("unexpected operationId: %v", operation["operationId"])
	}

	ref := operation["responses"].(map[string]interface{})["200"].(map[string]interface{})["content"].(map[string]interface{})["application/json"].(map[string]interface{})["schema"].(map[string]interface{})["$ref"]
	if ref != "#/components/schemas/Iam_Error" {
		t.Fatalf("unexpected ref rewrite: %v", ref)
	}
}

func TestApplyTokenEndpointEnrichment(t *testing.T) {
	doc := map[string]interface{}{
		"paths": map[string]interface{}{
			"/oidc/token": map[string]interface{}{
				"post": map[string]interface{}{
					"requestBody": map[string]interface{}{
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{"type": "object"},
							},
						},
					},
				},
			},
		},
	}

	applyTokenEndpointEnrichment(doc)
	post := doc["paths"].(map[string]interface{})["/oidc/token"].(map[string]interface{})["post"].(map[string]interface{})
	content := post["requestBody"].(map[string]interface{})["content"].(map[string]interface{})
	if _, ok := content["application/x-www-form-urlencoded"]; !ok {
		t.Fatalf("expected x-www-form-urlencoded content, got %v", content)
	}
	if _, ok := content["application/json"]; ok {
		t.Fatalf("expected application/json content to be removed")
	}
	if got := post["summary"]; got != "Generate access token" {
		t.Fatalf("unexpected summary: %v", got)
	}
}

func TestNormalizeNullableSchemas_RewritesAnyOfNull(t *testing.T) {
	doc := map[string]interface{}{
		"schema": map[string]interface{}{
			"description": "Account ID (optional)",
			"anyOf": []interface{}{
				map[string]interface{}{"type": "string"},
				map[string]interface{}{"type": "null"},
			},
		},
	}

	normalizeNullableSchemas(doc)
	schema := doc["schema"].(map[string]interface{})
	if _, ok := schema["anyOf"]; ok {
		t.Fatal("expected anyOf to be removed")
	}
	if got := schema["type"]; got != "string" {
		t.Fatalf("expected string type, got %v", got)
	}
	if got := schema["nullable"]; got != true {
		t.Fatalf("expected nullable=true, got %v", got)
	}
}

func TestNormalizeBinaryValueSchemaGoTypes_StampsXGoType(t *testing.T) {
	doc := map[string]interface{}{
		"components": map[string]interface{}{
			"schemas": map[string]interface{}{
				"Policy_ConfigurationOptionCreateType": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"value": map[string]interface{}{
							"type":   "string",
							"format": "binary",
						},
						"name": map[string]interface{}{
							"type": "string",
						},
					},
				},
				"UnrelatedSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"value": map[string]interface{}{
							"type": "string",
						},
					},
				},
			},
		},
	}

	normalizeBinaryValueSchemaGoTypes(doc)

	schemas := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})
	valueSchema := schemas["Policy_ConfigurationOptionCreateType"].(map[string]interface{})["properties"].(map[string]interface{})["value"].(map[string]interface{})
	if got := valueSchema["x-go-type"]; got != "interface{}" {
		t.Fatalf("expected x-go-type interface{}, got %v", got)
	}

	unrelatedValue := schemas["UnrelatedSchema"].(map[string]interface{})["properties"].(map[string]interface{})["value"].(map[string]interface{})
	if _, exists := unrelatedValue["x-go-type"]; exists {
		t.Fatalf("did not expect x-go-type on non-binary value schema")
	}
}

func TestValidateOpenAPIDocument_DetectsMissingComponentRef(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/openapi.json"
	data := `{
		"openapi": "3.0.3",
		"paths": {
			"/foo": {
				"get": {
					"operationId": "foo",
					"responses": {
						"200": {
							"description": "ok",
							"content": {
								"application/json": {
									"schema": {"$ref": "#/components/schemas/Missing"}
								}
							}
						}
					}
				}
			},
		},
		"components": {"schemas": {}}
	}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateOpenAPIDocument(path); err == nil {
		t.Fatal("expected missing component ref error")
	}
}

func TestValidateOpenAPIDocument_DetectsDuplicateOperationIDs(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/openapi.json"
	data := `{
		"openapi": "3.0.3",
		"paths": {
			"/foo": {"get": {"operationId": "dup", "responses": {"200": {"description": "ok"}}}},
			"/bar": {"get": {"operationId": "dup", "responses": {"200": {"description": "ok"}}}}
		},
		"components": {"schemas": {}}
	}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateOpenAPIDocument(path); err == nil {
		t.Fatal("expected duplicate operationId error")
	}
}

func TestValidateOpenAPIDocument_DetectsInvalidOperationIDFormat(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/openapi.json"
	data := `{
		"openapi": "3.0.3",
		"servers": [{"url": "https://api.flexera.{zone}", "variables": {"zone": {"default": "com"}}}],
		"paths": {
			"/foo": {"get": {"operationId": "bad-id", "responses": {"200": {"description": "ok"}}}}
		},
		"components": {"schemas": {}}
	}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateOpenAPIDocument(path); err == nil {
		t.Fatal("expected invalid operationId format error")
	}
}

func TestValidateOpenAPIDocument_DetectsInvalidParameter(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/openapi.json"
	data := `{
		"openapi": "3.0.3",
		"servers": [{"url": "https://api.flexera.{zone}", "variables": {"zone": {"default": "com"}}}],
		"paths": {
			"/foo": {
				"get": {
					"operationId": "good_id",
					"parameters": [{"name": "limit", "in": "body"}],
					"responses": {"200": {"description": "ok"}}
				}
			}
		},
		"components": {"schemas": {}}
	}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateOpenAPIDocument(path); err == nil {
		t.Fatal("expected invalid parameter error")
	}
}

func TestValidateOpenAPIDocument_DetectsMissingServerVariableDefinition(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/openapi.json"
	data := `{
		"openapi": "3.0.3",
		"servers": [{"url": "https://api.flexera.{zone}", "variables": {}}],
		"paths": {
			"/foo": {"get": {"operationId": "good_id", "responses": {"200": {"description": "ok"}}}}
		},
		"components": {"schemas": {}}
	}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateOpenAPIDocument(path); err == nil {
		t.Fatal("expected missing server variable definition error")
	}
}
