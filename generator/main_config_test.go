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

func TestConvertRequestBodiesUsesOperationConsumes(t *testing.T) {
	doc := map[string]interface{}{
		"paths": map[string]interface{}{
			"/files": map[string]interface{}{
				"post": map[string]interface{}{
					"consumes": []interface{}{"application/octet-stream"},
					"parameters": []interface{}{
						map[string]interface{}{
							"name":     "file",
							"in":       "body",
							"required": true,
							"schema":   map[string]interface{}{"type": "string", "format": "binary"},
						},
					},
				},
			},
		},
	}

	convertRequestBodies(doc)
	post := doc["paths"].(map[string]interface{})["/files"].(map[string]interface{})["post"].(map[string]interface{})
	body := post["requestBody"].(map[string]interface{})
	content := body["content"].(map[string]interface{})
	if _, ok := content["application/octet-stream"]; !ok {
		t.Fatalf("expected binary content type, got %v", content)
	}
	if _, ok := post["parameters"]; ok {
		t.Fatalf("expected body parameter to be removed, got %v", post["parameters"])
	}
	if _, ok := post["consumes"]; ok {
		t.Fatalf("expected Swagger consumes field to be removed, got %v", post["consumes"])
	}
}

func TestConvertSwagger2ToOpenAPI3_ResponsesAndCollectionFormat(t *testing.T) {
	swagger := map[string]interface{}{
		"swagger":  "2.0",
		"produces": []interface{}{"application/json"},
		"paths": map[string]interface{}{
			"/items": map[string]interface{}{
				"get": map[string]interface{}{
					"schemes": []interface{}{"http"},
					"parameters": []interface{}{
						map[string]interface{}{"name": "ids", "in": "query", "type": "array", "items": map[string]interface{}{"type": "string"}, "collectionFormat": "multi"},
						map[string]interface{}{"name": "tags", "in": "query", "type": "array", "items": map[string]interface{}{"type": "string"}, "collectionFormat": "csv"},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "OK",
							"schema":      map[string]interface{}{"$ref": "#/definitions/Item"},
							"headers":     map[string]interface{}{"Location": map[string]interface{}{"type": "string"}},
						},
					},
				},
			},
		},
		"definitions": map[string]interface{}{"Item": map[string]interface{}{"type": "object"}},
	}

	got := convertSwagger2ToOpenAPI3(swagger)
	op := got["paths"].(map[string]interface{})["/items"].(map[string]interface{})["get"].(map[string]interface{})
	if _, ok := op["schemes"]; ok {
		t.Fatalf("expected schemes to be removed")
	}
	resp := op["responses"].(map[string]interface{})["200"].(map[string]interface{})
	if _, ok := resp["schema"]; ok {
		t.Fatalf("expected Swagger response schema to be removed, got %v", resp)
	}
	schema := resp["content"].(map[string]interface{})["application/json"].(map[string]interface{})["schema"].(map[string]interface{})
	if schema["$ref"] != "#/components/schemas/Item" {
		t.Fatalf("unexpected response schema %v", schema)
	}
	header := resp["headers"].(map[string]interface{})["Location"].(map[string]interface{})
	if header["schema"].(map[string]interface{})["type"] != "string" || header["type"] != nil {
		t.Fatalf("expected header type wrapped in schema, got %v", header)
	}
	params := op["parameters"].([]interface{})
	for _, p := range params {
		if _, ok := p.(map[string]interface{})["collectionFormat"]; ok {
			t.Fatalf("expected collectionFormat to be removed, got %v", p)
		}
	}
	if params[1].(map[string]interface{})["explode"] != false {
		t.Fatalf("expected csv query param to set explode=false, got %v", params[1])
	}
}

func TestConvertSwagger2ToOpenAPI3_ResponseMediaTypes(t *testing.T) {
	tests := []struct {
		name              string
		globalProduces    interface{}
		operationProduces interface{}
		want              []string
	}{
		{
			name:           "inherits all document media types",
			globalProduces: []interface{}{"application/json", "application/xml", "application/gob"},
			want:           []string{"application/json", "application/xml", "application/gob"},
		},
		{
			name:              "operation overrides document media types",
			globalProduces:    []interface{}{"application/json", "application/gob"},
			operationProduces: []interface{}{"application/xml", "text/markdown"},
			want:              []string{"application/xml", "text/markdown"},
		},
		{
			name: "missing produces defaults to JSON",
			want: []string{"application/json"},
		},
		{
			name:           "empty document produces defaults to JSON",
			globalProduces: []interface{}{},
			want:           []string{"application/json"},
		},
		{
			name:              "empty operation produces does not inherit",
			globalProduces:    []interface{}{"application/xml"},
			operationProduces: []interface{}{},
			want:              []string{"application/json"},
		},
		{
			name:           "ignores empty invalid and duplicate entries",
			globalProduces: []interface{}{"", nil, 42, "application/xml", "application/xml", "application/gob"},
			want:           []string{"application/xml", "application/gob"},
		},
		{
			name:           "unusable entries default to JSON",
			globalProduces: []interface{}{"", nil, 42},
			want:           []string{"application/json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			responses := map[string]interface{}{
				"200": map[string]interface{}{"description": "OK", "schema": map[string]interface{}{"$ref": "#/definitions/Item"}},
				"400": map[string]interface{}{"description": "Error", "schema": map[string]interface{}{"$ref": "#/definitions/Item"}},
				"204": map[string]interface{}{"description": "No content"},
			}
			operation := map[string]interface{}{"responses": responses}
			if tt.operationProduces != nil {
				operation["produces"] = tt.operationProduces
			}
			swagger := map[string]interface{}{
				"swagger":     "2.0",
				"produces":    tt.globalProduces,
				"paths":       map[string]interface{}{"/items": map[string]interface{}{"get": operation}},
				"definitions": map[string]interface{}{"Item": map[string]interface{}{"type": "object"}},
			}
			convertSwagger2ToOpenAPI3(swagger)
			if _, ok := operation["produces"]; ok {
				t.Fatal("expected Swagger produces field to be removed")
			}
			for _, code := range []string{"200", "400"} {
				response := responses[code].(map[string]interface{})
				if _, ok := response["schema"]; ok {
					t.Fatalf("response %s still contains Swagger schema", code)
				}
				content := response["content"].(map[string]interface{})
				if len(content) != len(tt.want) {
					t.Fatalf("response %s content = %v, want media types %v", code, content, tt.want)
				}
				for _, mediaType := range tt.want {
					media, ok := content[mediaType].(map[string]interface{})
					if !ok {
						t.Fatalf("response %s missing media type %s", code, mediaType)
					}
					schema := media["schema"].(map[string]interface{})
					if schema["$ref"] != "#/components/schemas/Item" {
						t.Fatalf("response %s media type %s schema = %v", code, mediaType, schema)
					}
				}
			}
			if _, ok := responses["204"].(map[string]interface{})["content"]; ok {
				t.Fatal("expected response without schema to remain without content")
			}
		})
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

func TestAddRequestBody_InjectsBodyOnJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openapi.json")
	input := `{"paths":{"/files/{id}":{"post":{"operationId":"Files#create"}}}}`
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}

	requestBody := `{"required":true,"content":{"application/octet-stream":{"schema":{"type":"string","format":"binary"}}}}`
	if err := addRequestBody(path, "/files/{id}", "POST", requestBody); err != nil {
		t.Fatalf("addRequestBody() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	operation := doc["paths"].(map[string]interface{})["/files/{id}"].(map[string]interface{})["post"].(map[string]interface{})
	rb, ok := operation["requestBody"].(map[string]interface{})
	if !ok {
		t.Fatalf("requestBody not injected, operation = %#v", operation)
	}
	if rb["required"] != true {
		t.Fatalf("requestBody.required = %v, want true", rb["required"])
	}
}

func TestAddRequestBody_InjectsBodyOnYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openapi.yaml")
	input := "paths:\n    /files/{id}:\n        post:\n            operationId: Files#create\n"
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}

	requestBody := `{"required":true,"content":{"application/octet-stream":{"schema":{"type":"string","format":"binary"}}}}`
	if err := addRequestBody(path, "/files/{id}", "post", requestBody); err != nil {
		t.Fatalf("addRequestBody() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "requestBody") || !strings.Contains(string(data), "application/octet-stream") {
		t.Fatalf("YAML output missing injected requestBody: %s", data)
	}
}

func TestAddRequestBody_MissingPathIsActionable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openapi.json")
	if err := os.WriteFile(path, []byte(`{"paths":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := addRequestBody(path, "/missing", "post", `{"required":true}`)
	if err == nil || !strings.Contains(err.Error(), "upstream document may have changed") {
		t.Fatalf("addRequestBody() error = %v, want upstream-change guidance", err)
	}
}

func TestAddRequestBody_ExistingRequestBodyIsActionable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "openapi.json")
	input := `{"paths":{"/files/{id}":{"post":{"requestBody":{"required":true,"content":{}}}}}}`
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}

	err := addRequestBody(path, "/files/{id}", "post", `{"required":true}`)
	if err == nil || !strings.Contains(err.Error(), "already declares a requestBody") {
		t.Fatalf("addRequestBody() error = %v, want already-declares guidance", err)
	}
}
