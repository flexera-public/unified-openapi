package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const unifiedSpecRelativePath = "openapi3.json"
const unifiedSpecYAMLPath = "openapi3.yaml"

var componentSections = []string{"schemas", "parameters", "responses", "requestBodies", "headers", "examples", "links", "callbacks"}
var operationMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}
var operationIDPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
var serverVariablePattern = regexp.MustCompile(`\{([^{}]+)\}`)

func handleMerge(baseDir, outputDir string, args []string) {
	target := "enabled-flexera"
	if len(args) > 0 {
		target = args[0]
	}

	config, err := loadConfig(baseDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	specs, err := selectFlexeraSpecsForMerge(config, target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error selecting specs for merge: %v\n", err)
		os.Exit(1)
	}

	unified, err := buildUnifiedSpec(baseDir, specs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error building unified spec: %v\n", err)
		os.Exit(1)
	}

	outputPath := filepath.Join(outputDir, unifiedSpecRelativePath)
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating unified output directory: %v\n", err)
		os.Exit(1)
	}

	data, err := json.MarshalIndent(unified, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding unified spec: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing unified spec: %v\n", err)
		os.Exit(1)
	}

	if err := validateOpenAPIDocument(outputPath); err != nil {
		fmt.Fprintf(os.Stderr, "Unified spec validation failed: %v\n", err)
		os.Exit(1)
	}

	// Write YAML companion file.
	yamlPath := filepath.Join(outputDir, unifiedSpecYAMLPath)
	yamlData, err := yaml.Marshal(unified)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding unified spec as YAML: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(yamlPath, yamlData, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing unified spec YAML: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Unified Flexera spec written to %s and %s\n",
		outputPath, yamlPath)
}

func buildUnifiedSpec(baseDir string, specs []SpecConfig) (map[string]interface{}, error) {
	unified := newUnifiedSpec()
	paths := unified["paths"].(map[string]interface{})
	components := unified["components"].(map[string]interface{})
	tagsByName := map[string]map[string]interface{}{}

	for _, spec := range specs {
		doc, err := loadOpenAPIDocument(filepath.Join(baseDir, spec.Output.Path, spec.Output.Filename))
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", spec.ID, err)
		}

		normalized, err := normalizeServiceDocument(doc, spec)
		if err != nil {
			return nil, fmt.Errorf("normalize %s: %w", spec.ID, err)
		}

		if err := mergeDocumentIntoUnified(normalized, paths, components, tagsByName); err != nil {
			return nil, fmt.Errorf("merge %s: %w", spec.ID, err)
		}
	}

	tokenDocPath := filepath.Join(baseDir, "sources/flexera/okta_token_service/openapi.json")
	if _, err := os.Stat(tokenDocPath); err == nil {
		doc, err := loadOpenAPIDocument(tokenDocPath)
		if err != nil {
			return nil, fmt.Errorf("load okta token service: %w", err)
		}

		normalized, err := normalizeTokenDocument(doc)
		if err != nil {
			return nil, fmt.Errorf("normalize okta token service: %w", err)
		}

		if err := mergeDocumentIntoUnified(normalized, paths, components, tagsByName); err != nil {
			return nil, fmt.Errorf("merge okta token service: %w", err)
		}
	}

	unified["tags"] = sortTags(tagsByName)
	annotateForCLI(unified)

	// Apply optional TF override file (tf-overrides.yaml). Missing file =
	// pure heuristic mode (default). Present file MUST validate.
	overrides, err := loadTFOverrides(baseDir)
	if err != nil {
		return nil, fmt.Errorf("load tf overrides: %w", err)
	}
	if err := applyTFOverrides(unified, overrides); err != nil {
		return nil, fmt.Errorf("apply tf overrides: %w", err)
	}
	return unified, nil
}

func newUnifiedSpec() map[string]interface{} {
	return map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title":       "Flexera One API",
			"version":     "1.0.0",
			"description": "Unified Flexera One API surface assembled from service-specific OpenAPI documents and enriched for documentation and SDK generation.",
		},
		"servers": []interface{}{
			apiServerObject("api"),
			apiTestServerObject("api"),
		},
		"security": []interface{}{
			map[string]interface{}{"FlexeraBearerAuth": []interface{}{}},
		},
		"paths": map[string]interface{}{},
		"components": map[string]interface{}{
			"schemas":       map[string]interface{}{},
			"parameters":    map[string]interface{}{},
			"responses":     map[string]interface{}{},
			"requestBodies": map[string]interface{}{},
			"headers":       map[string]interface{}{},
			"examples":      map[string]interface{}{},
			"links":         map[string]interface{}{},
			"callbacks":     map[string]interface{}{},
			"securitySchemes": map[string]interface{}{
				"FlexeraBearerAuth": map[string]interface{}{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "JWT",
					"description":  "Bearer access token obtained from the Flexera login token service.",
				},
			},
		},
		"tags": []interface{}{},
		"x-flexera-auth": map[string]interface{}{
			"tokenEndpoint": map[string]interface{}{
				"path":         "/oidc/token",
				"hostTemplate": "https://login.flexera.{zone}",
				"zones": map[string]interface{}{
					"nam":  "com",
					"eu":   "eu",
					"apac": "au",
				},
				"testHost":  "https://login.flexeratest.com",
				"testNotes": "The flexeratest.com staging environment does not fit the flexera.{zone} template; use this literal host for the 'test' zone.",
			},
			"grants": map[string]interface{}{
				"serviceAccount": map[string]interface{}{
					"grantType":          "client_credentials",
					"requestContentType": "application/x-www-form-urlencoded",
					"requiredFields":     []interface{}{"client_id", "client_secret"},
				},
				"user": map[string]interface{}{
					"grantType":          "refresh_token",
					"requestContentType": "application/x-www-form-urlencoded",
					"requiredFields":     []interface{}{"refresh_token"},
				},
			},
			"accessToken": map[string]interface{}{
				"header": "Authorization",
				"type":   "bearer",
				"format": "JWT",
			},
			"renewal": map[string]interface{}{
				"strategy": "repeat-token-request",
				"notes":    "Access tokens expire hourly; request a new token from the same login endpoint.",
			},
		},
		"x-flexera-pagination": map[string]interface{}{
			"type":          "cursor",
			"nextField":     "nextPage",
			"previousField": "prevPage",
			"notes":         "Follow the nextPage URL until it is absent.",
		},
	}
}

func apiServerObject(subdomain string) map[string]interface{} {
	return map[string]interface{}{
		"url":         fmt.Sprintf("https://%s.flexera.{zone}", subdomain),
		"description": "Flexera One API",
		"variables":   zoneVariables(),
	}
}

// apiTestServerObject returns the literal flexeratest.com staging API server
// entry. The test environment uses a different host shape (flexeratest.com
// vs flexera.{zone}) so it cannot be expressed by the production server
// variable enum and must be emitted as a separate server URL.
func apiTestServerObject(subdomain string) map[string]interface{} {
	return map[string]interface{}{
		"url":         fmt.Sprintf("https://%s.flexeratest.com", subdomain),
		"description": "Flexera One API (test / staging)",
	}
}

func loginServerObject() map[string]interface{} {
	return map[string]interface{}{
		"url":         "https://login.flexera.{zone}",
		"description": "Flexera login token service",
		"variables":   zoneVariables(),
	}
}

// loginTestServerObject returns the literal staging login server entry. See
// apiTestServerObject for why this is separate from the zoned login server.
func loginTestServerObject() map[string]interface{} {
	return map[string]interface{}{
		"url":         "https://login.flexeratest.com",
		"description": "Flexera login token service (test / staging)",
	}
}

func zoneVariables() map[string]interface{} {
	return map[string]interface{}{
		"zone": map[string]interface{}{
			"default":     "com",
			"enum":        []interface{}{"com", "eu", "au"},
			"description": "com = NAM, eu = EU, au = APAC",
		},
	}
}

func selectFlexeraSpecsForMerge(config *Config, target string) ([]SpecConfig, error) {
	var specs []SpecConfig
	switch target {
	case "all", "enabled-flexera":
		for _, spec := range config.Specs {
			if spec.Vendor == "flexera" {
				specs = append(specs, spec)
			}
		}
	default:
		spec := findSpec(config, target)
		if spec == nil {
			return nil, fmt.Errorf("spec not found: %s", target)
		}
		if spec.Vendor != "flexera" {
			return nil, fmt.Errorf("merge only supports Flexera specs, got vendor %s", spec.Vendor)
		}
		specs = append(specs, *spec)
	}

	if len(specs) == 0 {
		return nil, fmt.Errorf("no Flexera specs selected")
	}
	return specs, nil
}

func loadOpenAPIDocument(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func normalizeServiceDocument(doc map[string]interface{}, spec SpecConfig) (map[string]interface{}, error) {
	clone, err := cloneDocument(doc)
	if err != nil {
		return nil, err
	}

	normalizeNullableSchemas(clone)
	normalizeBinaryValueSchemaGoTypes(clone)

	namespace := componentNamespace(spec.Service)
	renameMap := prefixComponentNames(clone, namespace)
	rewriteRefs(clone, renameMap)
	prefixOperationIDs(clone, namespace)
	normalizeOperationTags(clone, spec.Name)
	applyBearerSecurity(clone)
	removeSourceSecuritySchemes(clone)
	return clone, nil
}

func normalizeTokenDocument(doc map[string]interface{}) (map[string]interface{}, error) {
	tokenPathItem, ok := lookupPathItem(doc, "/oidc/token")
	if !ok {
		return nil, fmt.Errorf("token endpoint /oidc/token not found")
	}

	normalized := map[string]interface{}{
		"paths": map[string]interface{}{
			"/oidc/token": tokenPathItem,
		},
		"components": map[string]interface{}{
			"schemas": map[string]interface{}{},
		},
		"tags": []interface{}{
			map[string]interface{}{
				"name":        "Authentication",
				"description": "Access-token acquisition and renewal for Flexera One APIs.",
			},
		},
	}

	if components, ok := doc["components"].(map[string]interface{}); ok {
		if schemas, ok := components["schemas"].(map[string]interface{}); ok {
			targetSchemas := map[string]interface{}{}
			for _, name := range []string{"TokenRequestBody", "TokenResponseBody", "Error"} {
				if schema, exists := schemas[name]; exists {
					targetSchemas[name] = schema
				}
			}
			normalized["components"].(map[string]interface{})["schemas"] = targetSchemas
		}
	}

	namespace := componentNamespace("auth")
	normalizeNullableSchemas(normalized)
	normalizeBinaryValueSchemaGoTypes(normalized)
	renameMap := prefixComponentNames(normalized, namespace)
	rewriteRefs(normalized, renameMap)
	prefixOperationIDs(normalized, namespace)
	applyTokenEndpointEnrichment(normalized)
	removeSourceSecuritySchemes(normalized)
	return normalized, nil
}

// normalizeBinaryValueSchemaGoTypes patches schema properties named "value"
// that are incorrectly modeled as type=string, format=binary while the API
// actually carries arbitrary JSON scalars/arrays/objects. We keep the OpenAPI
// shape intact for downstream heuristics, but stamp x-go-type so oapi-codegen
// emits interface{} instead of openapi_types.File in generated clients.
func normalizeBinaryValueSchemaGoTypes(obj interface{}) {
	switch v := obj.(type) {
	case map[string]interface{}:
		if props, ok := v["properties"].(map[string]interface{}); ok {
			if rawValueSchema, exists := props["value"]; exists {
				if valueSchema, ok := rawValueSchema.(map[string]interface{}); ok {
					if typ, _ := valueSchema["type"].(string); typ == "string" {
						if format, _ := valueSchema["format"].(string); format == "binary" {
							valueSchema["x-go-type"] = "interface{}"
						}
					}
				}
			}
		}
		for _, child := range v {
			normalizeBinaryValueSchemaGoTypes(child)
		}
	case []interface{}:
		for _, child := range v {
			normalizeBinaryValueSchemaGoTypes(child)
		}
	}
}

func cloneDocument(doc map[string]interface{}) (map[string]interface{}, error) {
	data, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}

	var clone map[string]interface{}
	if err := json.Unmarshal(data, &clone); err != nil {
		return nil, err
	}
	return clone, nil
}

func componentNamespace(service string) string {
	parts := strings.FieldsFunc(service, func(r rune) bool {
		return r == '_' || r == '-' || r == ' ' || r == '/'
	})
	if len(parts) == 0 {
		return "Spec"
	}
	var b strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		if len(part) > 1 {
			b.WriteString(part[1:])
		}
	}
	if b.Len() == 0 {
		return "Spec"
	}
	return b.String()
}

func prefixComponentNames(doc map[string]interface{}, namespace string) map[string]string {
	renameMap := map[string]string{}
	components, ok := doc["components"].(map[string]interface{})
	if !ok {
		return renameMap
	}

	for _, section := range append(componentSections, "securitySchemes") {
		sectionMap, ok := components[section].(map[string]interface{})
		if !ok {
			continue
		}

		renamed := map[string]interface{}{}
		for name, value := range sectionMap {
			newName := namespace + "_" + name
			renamed[newName] = value
			renameMap[fmt.Sprintf("#/components/%s/%s", section, name)] = fmt.Sprintf("#/components/%s/%s", section, newName)
		}
		components[section] = renamed
	}

	return renameMap
}

func rewriteRefs(obj interface{}, renameMap map[string]string) {
	switch v := obj.(type) {
	case map[string]interface{}:
		for key, value := range v {
			if key == "$ref" {
				if ref, ok := value.(string); ok {
					if newRef, exists := renameMap[ref]; exists {
						v[key] = newRef
					}
				}
				continue
			}
			rewriteRefs(value, renameMap)
		}
	case []interface{}:
		for _, item := range v {
			rewriteRefs(item, renameMap)
		}
	}
}

func prefixOperationIDs(doc map[string]interface{}, namespace string) {
	paths, ok := doc["paths"].(map[string]interface{})
	if !ok {
		return
	}

	for _, pathItemValue := range paths {
		pathItem, ok := pathItemValue.(map[string]interface{})
		if !ok {
			continue
		}
		for _, method := range operationMethods {
			operation, ok := pathItem[method].(map[string]interface{})
			if !ok {
				continue
			}
			operationID, _ := operation["operationId"].(string)
			if operationID == "" {
				continue
			}
			operation["operationId"] = namespace + "_" + sanitizeIdentifier(operationID)
		}
	}
}

func sanitizeIdentifier(value string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteRune('_')
			lastUnderscore = true
		}
	}
	result := strings.Trim(b.String(), "_")
	if result == "" {
		return "operation"
	}
	return result
}

func normalizeOperationTags(doc map[string]interface{}, serviceName string) {
	paths, ok := doc["paths"].(map[string]interface{})
	if !ok {
		return
	}

	fallbackTag := serviceName
	for _, pathItemValue := range paths {
		pathItem, ok := pathItemValue.(map[string]interface{})
		if !ok {
			continue
		}
		for _, method := range operationMethods {
			operation, ok := pathItem[method].(map[string]interface{})
			if !ok {
				continue
			}
			tags, ok := operation["tags"].([]interface{})
			if !ok || len(tags) == 0 {
				operation["tags"] = []interface{}{fallbackTag}
			}
		}
	}
}

func applyBearerSecurity(doc map[string]interface{}) {
	paths, ok := doc["paths"].(map[string]interface{})
	if !ok {
		return
	}

	security := []interface{}{map[string]interface{}{"FlexeraBearerAuth": []interface{}{}}}
	for _, pathItemValue := range paths {
		pathItem, ok := pathItemValue.(map[string]interface{})
		if !ok {
			continue
		}
		for _, method := range operationMethods {
			operation, ok := pathItem[method].(map[string]interface{})
			if !ok {
				continue
			}
			operation["security"] = cloneInterfaceSlice(security)
		}
	}
}

func applyTokenEndpointEnrichment(doc map[string]interface{}) {
	pathItem, ok := lookupPathItem(doc, "/oidc/token")
	if !ok {
		return
	}

	operation, ok := pathItem["post"].(map[string]interface{})
	if !ok {
		return
	}

	operation["tags"] = []interface{}{"Authentication"}
	operation["summary"] = "Generate access token"
	operation["description"] = "Generate a Flexera One access token for either a service account (client_credentials) or a user refresh token (refresh_token)."
	operation["security"] = []interface{}{}
	operation["servers"] = []interface{}{loginServerObject(), loginTestServerObject()}

	requestBody, ok := operation["requestBody"].(map[string]interface{})
	if ok {
		content, ok := requestBody["content"].(map[string]interface{})
		if ok {
			if jsonContent, exists := content["application/json"]; exists {
				delete(content, "application/json")
				content["application/x-www-form-urlencoded"] = jsonContent
			}
		}
	}
}

func removeSourceSecuritySchemes(doc map[string]interface{}) {
	components, ok := doc["components"].(map[string]interface{})
	if !ok {
		return
	}
	delete(components, "securitySchemes")
}

func mergeDocumentIntoUnified(doc map[string]interface{}, unifiedPaths map[string]interface{}, unifiedComponents map[string]interface{}, tagsByName map[string]map[string]interface{}) error {
	if paths, ok := doc["paths"].(map[string]interface{}); ok {
		for path, pathItem := range paths {
			if _, exists := unifiedPaths[path]; exists {
				return fmt.Errorf("path already exists: %s", path)
			}
			unifiedPaths[path] = pathItem
		}
	}

	if components, ok := doc["components"].(map[string]interface{}); ok {
		for _, section := range componentSections {
			sourceSection, ok := components[section].(map[string]interface{})
			if !ok {
				continue
			}
			targetSection, _ := unifiedComponents[section].(map[string]interface{})
			if targetSection == nil {
				targetSection = map[string]interface{}{}
				unifiedComponents[section] = targetSection
			}
			for key, value := range sourceSection {
				if _, exists := targetSection[key]; exists {
					return fmt.Errorf("duplicate component %s/%s", section, key)
				}
				targetSection[key] = value
			}
		}
	}

	if tags, ok := doc["tags"].([]interface{}); ok {
		for _, tagValue := range tags {
			tag, ok := tagValue.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := tag["name"].(string)
			if name == "" {
				continue
			}
			if _, exists := tagsByName[name]; !exists {
				tagsByName[name] = tag
			}
		}
	}

	return nil
}

func sortTags(tagsByName map[string]map[string]interface{}) []interface{} {
	names := make([]string, 0, len(tagsByName))
	for name := range tagsByName {
		names = append(names, name)
	}
	sort.Strings(names)

	result := make([]interface{}, 0, len(names))
	for _, name := range names {
		result = append(result, tagsByName[name])
	}
	return result
}

func lookupPathItem(doc map[string]interface{}, path string) (map[string]interface{}, bool) {
	paths, ok := doc["paths"].(map[string]interface{})
	if !ok {
		return nil, false
	}
	pathItem, ok := paths[path].(map[string]interface{})
	return pathItem, ok
}

func cloneInterfaceSlice(values []interface{}) []interface{} {
	cloned := make([]interface{}, len(values))
	copy(cloned, values)
	return cloned
}

func validateOpenAPIDocument(filePath string) error {
	doc, err := loadOpenAPIDocument(filePath)
	if err != nil {
		return err
	}

	if _, ok := doc["openapi"].(string); !ok {
		return fmt.Errorf("missing openapi version")
	}

	if _, ok := doc["paths"].(map[string]interface{}); !ok {
		return fmt.Errorf("missing paths object")
	}

	if err := validateUniqueOperationIDs(doc); err != nil {
		return err
	}

	if err := validateLocalRefs(doc); err != nil {
		return err
	}

	if err := validateParameters(doc); err != nil {
		return err
	}

	if err := validateServerVariables(doc); err != nil {
		return err
	}

	return nil
}

func validateUniqueOperationIDs(doc map[string]interface{}) error {
	seen := map[string]string{}
	paths, _ := doc["paths"].(map[string]interface{})
	for path, pathItemValue := range paths {
		pathItem, ok := pathItemValue.(map[string]interface{})
		if !ok {
			continue
		}
		for _, method := range operationMethods {
			operation, ok := pathItem[method].(map[string]interface{})
			if !ok {
				continue
			}
			operationID, _ := operation["operationId"].(string)
			if operationID == "" {
				continue
			}
			if !operationIDPattern.MatchString(operationID) {
				return fmt.Errorf("invalid operationId %q at %s %s", operationID, strings.ToUpper(method), path)
			}
			location := fmt.Sprintf("%s %s", strings.ToUpper(method), path)
			if previous, exists := seen[operationID]; exists {
				return fmt.Errorf("duplicate operationId %q used by %s and %s", operationID, previous, location)
			}
			seen[operationID] = location
		}
	}
	return nil
}

func validateLocalRefs(doc map[string]interface{}) error {
	refs := collectLocalRefs(doc)
	for _, ref := range refs {
		parts := strings.Split(ref, "/")
		if len(parts) != 4 || parts[0] != "#" || parts[1] != "components" {
			continue
		}
		components, _ := doc["components"].(map[string]interface{})
		section, _ := components[parts[2]].(map[string]interface{})
		if section == nil {
			return fmt.Errorf("missing component section referenced by %s", ref)
		}
		if _, exists := section[parts[3]]; !exists {
			return fmt.Errorf("missing component referenced by %s", ref)
		}
	}
	return nil
}

func collectLocalRefs(obj interface{}) []string {
	refs := []string{}
	switch v := obj.(type) {
	case map[string]interface{}:
		for key, value := range v {
			if key == "$ref" {
				if ref, ok := value.(string); ok && strings.HasPrefix(ref, "#/components/") {
					refs = append(refs, ref)
				}
				continue
			}
			refs = append(refs, collectLocalRefs(value)...)
		}
	case []interface{}:
		for _, item := range v {
			refs = append(refs, collectLocalRefs(item)...)
		}
	}
	return refs
}

func normalizeNullableSchemas(obj interface{}) {
	switch v := obj.(type) {
	case map[string]interface{}:
		normalizeNullableAlternatives(v, "anyOf")
		normalizeNullableAlternatives(v, "oneOf")
		for _, value := range v {
			normalizeNullableSchemas(value)
		}
	case []interface{}:
		for _, item := range v {
			normalizeNullableSchemas(item)
		}
	}
}

func normalizeNullableAlternatives(schema map[string]interface{}, key string) {
	rawAlternatives, ok := schema[key]
	if !ok {
		return
	}

	alternatives, ok := rawAlternatives.([]interface{})
	if !ok || len(alternatives) < 2 {
		return
	}

	var nonNullSchema map[string]interface{}
	nullCount := 0
	for _, rawAlternative := range alternatives {
		alternative, ok := rawAlternative.(map[string]interface{})
		if !ok {
			return
		}
		if alternativeType, _ := alternative["type"].(string); alternativeType == "null" {
			nullCount++
			continue
		}
		if nonNullSchema != nil {
			return
		}
		nonNullSchema = alternative
	}

	if nullCount == 0 || nonNullSchema == nil {
		return
	}

	delete(schema, key)
	for nestedKey, nestedValue := range nonNullSchema {
		if _, exists := schema[nestedKey]; !exists {
			schema[nestedKey] = nestedValue
		}
	}
	schema["nullable"] = true
}

func validateParameters(doc map[string]interface{}) error {
	paths, _ := doc["paths"].(map[string]interface{})
	for path, pathItemValue := range paths {
		pathItem, ok := pathItemValue.(map[string]interface{})
		if !ok {
			continue
		}

		if err := validateParameterList(pathItem["parameters"], fmt.Sprintf("path %s", path)); err != nil {
			return err
		}

		for _, method := range operationMethods {
			operation, ok := pathItem[method].(map[string]interface{})
			if !ok {
				continue
			}
			if err := validateParameterList(operation["parameters"], fmt.Sprintf("%s %s", strings.ToUpper(method), path)); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateParameterList(raw interface{}, location string) error {
	if raw == nil {
		return nil
	}

	parameters, ok := raw.([]interface{})
	if !ok {
		return fmt.Errorf("parameters must be an array at %s", location)
	}

	for idx, rawParameter := range parameters {
		parameter, ok := rawParameter.(map[string]interface{})
		if !ok {
			return fmt.Errorf("parameter %d must be an object at %s", idx, location)
		}
		if _, hasRef := parameter["$ref"]; hasRef {
			continue
		}
		name, _ := parameter["name"].(string)
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("parameter %d missing name at %s", idx, location)
		}
		paramIn, _ := parameter["in"].(string)
		switch paramIn {
		case "query", "header", "path", "cookie":
		default:
			return fmt.Errorf("parameter %q has invalid in=%q at %s", name, paramIn, location)
		}
	}

	return nil
}

func validateServerVariables(doc map[string]interface{}) error {
	if err := validateServersList(doc["servers"], "root"); err != nil {
		return err
	}

	paths, _ := doc["paths"].(map[string]interface{})
	for path, pathItemValue := range paths {
		pathItem, ok := pathItemValue.(map[string]interface{})
		if !ok {
			continue
		}

		if err := validateServersList(pathItem["servers"], fmt.Sprintf("path %s", path)); err != nil {
			return err
		}

		for _, method := range operationMethods {
			operation, ok := pathItem[method].(map[string]interface{})
			if !ok {
				continue
			}
			if err := validateServersList(operation["servers"], fmt.Sprintf("%s %s", strings.ToUpper(method), path)); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateServersList(raw interface{}, location string) error {
	if raw == nil {
		return nil
	}

	servers, ok := raw.([]interface{})
	if !ok {
		return fmt.Errorf("servers must be an array at %s", location)
	}

	for idx, rawServer := range servers {
		server, ok := rawServer.(map[string]interface{})
		if !ok {
			return fmt.Errorf("server %d must be an object at %s", idx, location)
		}
		url, _ := server["url"].(string)
		if strings.TrimSpace(url) == "" {
			return fmt.Errorf("server %d missing url at %s", idx, location)
		}
		variables, _ := server["variables"].(map[string]interface{})
		for _, match := range serverVariablePattern.FindAllStringSubmatch(url, -1) {
			variableName := match[1]
			if _, exists := variables[variableName]; !exists {
				return fmt.Errorf("server variable %q missing definition at %s", variableName, location)
			}
		}
	}

	return nil
}
