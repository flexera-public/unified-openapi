package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func resolveSpecsDir(args []string) (string, []string, error) {
	var baseDir string
	cleanedArgs := make([]string, 0, len(args))
	flagFound := false

	// Scan for --openapi_specs_dir flag
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--openapi_specs_dir" {
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("flag --openapi_specs_dir requires a value")
			}
			baseDir = args[i+1]
			flagFound = true
			i++ // skip next arg (the value)
		} else if strings.HasPrefix(arg, "--openapi_specs_dir=") {
			baseDir = strings.TrimPrefix(arg, "--openapi_specs_dir=")
			flagFound = true
		} else {
			cleanedArgs = append(cleanedArgs, arg)
		}
	}

	// If no flag, check environment variable
	if !flagFound {
		if envDir := os.Getenv("OPENAPI_SPECS_DIR"); envDir != "" {
			baseDir = envDir
		} else {
			baseDir = "."
		}
	}

	// Normalize path
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		baseDir = "."
	}

	// Verify directory exists
	info, err := os.Stat(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, fmt.Errorf("specs directory does not exist: %s", baseDir)
		}
		return "", nil, fmt.Errorf("failed to access specs directory %s: %w", baseDir, err)
	}

	if !info.IsDir() {
		return "", nil, fmt.Errorf("path is not a directory: %s", baseDir)
	}

	return baseDir, cleanedArgs, nil
}

// resolveOutputDir extracts the --output-dir flag from args.
// Controls where merge writes openapi3.json/yaml.
// Defaults to baseDir if not specified.
func resolveOutputDir(args []string, baseDir string) (string, []string, error) {
	outputDir := baseDir
	cleanedArgs := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--output-dir" {
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("flag --output-dir requires a value")
			}
			outputDir = args[i+1]
			i++ // skip the value
		} else if strings.HasPrefix(arg, "--output-dir=") {
			outputDir = strings.TrimPrefix(arg, "--output-dir=")
		} else {
			cleanedArgs = append(cleanedArgs, arg)
		}
	}

	outputDir = strings.TrimSpace(outputDir)
	if outputDir == "" {
		outputDir = baseDir
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", nil, fmt.Errorf("failed to create output directory %s: %w", outputDir, err)
	}

	return outputDir, cleanedArgs, nil
}

// Config represents the specs.yaml configuration
type Config struct {
	Specs []SpecConfig `yaml:"specs"`
}

// SpecConfig represents a single API specification
type SpecConfig struct {
	ID         string           `yaml:"id"`
	Name       string           `yaml:"name"`
	Vendor     string           `yaml:"vendor"`
	Service    string           `yaml:"service"`
	Version    string           `yaml:"version"`
	Source     SourceConfig     `yaml:"source"`
	Format     string           `yaml:"format"`
	Output     OutputConfig     `yaml:"output"`
	Processing []ProcessingStep `yaml:"processing"`

	// IncludeInMerge opts a non-Flexera-vendor spec into the unified merge
	// output. Flexera-vendor specs are always merge-eligible and do not need
	// to set this; it exists so specs from other vendors (e.g. rightscale)
	// can be merged in individually once their endpoints have been reviewed
	// for overlap with existing Flexera equivalents.
	IncludeInMerge bool `yaml:"include_in_merge,omitempty"`

	// MergeExcludePaths lists source-document path templates (as they
	// appear in this spec's own `paths` object, e.g. "/orgs/{org}/budgets")
	// that should be dropped before merging into the unified spec. Use this
	// to omit endpoints that have already been migrated/replaced by an
	// equivalent Flexera API and would otherwise duplicate functionality.
	MergeExcludePaths []MergeExcludePath `yaml:"merge_exclude_paths,omitempty"`

	// MergeServers overrides the unified spec's canonical `servers` entry
	// for every path contributed by this spec. Use this when a spec's real
	// upstream host differs from the default Flexera One API gateway (e.g.
	// api.flexera.{zone}) — the override is stamped onto each path item so
	// it takes precedence over the document-level `servers` array per the
	// OpenAPI 3 spec.
	MergeServers []ServerConfig `yaml:"merge_servers,omitempty"`
}

// ServerConfig represents an OpenAPI Server Object.
type ServerConfig struct {
	URL         string                    `yaml:"url"`
	Description string                    `yaml:"description,omitempty"`
	Variables   map[string]ServerVariable `yaml:"variables,omitempty"`
}

// ServerVariable represents an OpenAPI Server Variable Object.
type ServerVariable struct {
	Default     string   `yaml:"default"`
	Enum        []string `yaml:"enum,omitempty"`
	Description string   `yaml:"description,omitempty"`
}

// MergeExcludePath documents a single path dropped from the merged spec,
// along with the reason (typically pointing at the replacing Flexera API).
type MergeExcludePath struct {
	Path   string `yaml:"path"`
	Reason string `yaml:"reason"`
}

// SourceConfig represents the source of the API spec
type SourceConfig struct {
	Type        string `yaml:"type"`
	URL         string `yaml:"url"`
	Description string `yaml:"description,omitempty"`
}

// OutputConfig represents the output configuration
type OutputConfig struct {
	Path     string `yaml:"path"`
	Filename string `yaml:"filename"`
}

// ProcessingStep represents a processing step
type ProcessingStep struct {
	Type        string `yaml:"type"`
	Output      string `yaml:"output,omitempty"`
	From        string `yaml:"from,omitempty"`
	To          string `yaml:"to,omitempty"`
	Input       string `yaml:"input,omitempty"`
	Format      string `yaml:"format,omitempty"`
	Pattern     string `yaml:"pattern,omitempty"`
	Replacement string `yaml:"replacement,omitempty"`
	Reason      string `yaml:"reason,omitempty"`
	Notes       string `yaml:"notes,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Resolve specs directory from flags/env/default
	baseDir, cleanedArgs, err := resolveSpecsDir(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Resolve optional --output-dir flag (defaults to baseDir).
	// Controls where merge writes openapi3.json and openapi3.yaml.
	outputDir, cleanedArgs, err := resolveOutputDir(cleanedArgs, baseDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(cleanedArgs) < 1 {
		printUsage()
		os.Exit(1)
	}

	command := cleanedArgs[0]

	switch command {
	case "fetch":
		handleFetch(baseDir, cleanedArgs[1:])
	case "merge":
		handleMerge(baseDir, outputDir, cleanedArgs[1:])
	case "list":
		handleList(baseDir)
	case "info":
		handleInfo(baseDir, cleanedArgs[1:])
	case "validate":
		handleValidate(baseDir, cleanedArgs[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`OpenAPI Spec Generator

Usage:
  generator [--openapi_specs_dir <path>] [--output-dir <path>] <command> [arguments]

Global Flags:
  --openapi_specs_dir <path>  Base directory for specs.yaml config, sources, and
                              tf-overrides files (default: current directory).
                              Can also be set via OPENAPI_SPECS_DIR env var.
  --output-dir <path>         Directory where openapi3.json/yaml are written
                              (default: same as --openapi_specs_dir).
                              Pass ".." when running from the generator/ subdir
                              to write the spec artifacts to the repo root.

Commands:
  fetch <spec-id|all>     Fetch and process OpenAPI specification(s)
  merge [target]          Merge Flexera specs into a unified OpenAPI artifact
  list                    List all configured specifications
  info <spec-id>          Show detailed information about a specification
  validate <spec-id|all>  Validate specification files exist (or 'unified')

Examples:
  generator merge enabled-flexera
  generator --output-dir .. merge enabled-flexera
  generator --openapi_specs_dir /specs fetch all
  generator info rightscale-governance
  OPENAPI_SPECS_DIR=/var/lib/specs generator validate all`)
}

func loadConfig(baseDir string) (*Config, error) {
	configPath := filepath.Join(baseDir, "specs.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", configPath, err)
	}

	return &config, nil
}

func handleFetch(baseDir string, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Error: spec-id required")
		printUsage()
		os.Exit(1)
	}

	target := args[0]
	config, err := loadConfig(baseDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if target == "all" {
		for _, spec := range config.Specs {
			if spec.Source.Type == "manual" {
				fmt.Printf("Skipping %s (manual)\n", spec.ID)
				continue
			}
			fmt.Printf("Fetching %s...\n", spec.ID)
			if err := fetchSpec(baseDir, spec); err != nil {
				fmt.Fprintf(os.Stderr, "  Error: %v\n", err)
			} else {
				fmt.Printf("  ✓ Success\n")
			}
		}
	} else {
		spec := findSpec(config, target)
		if spec == nil {
			fmt.Fprintf(os.Stderr, "Error: spec not found: %s\n", target)
			os.Exit(1)
		}

		if spec.Source.Type == "manual" {
			fmt.Printf("Spec %s requires manual copying:\n", spec.ID)
			fmt.Printf("  %s\n", spec.Source.Description)
			fmt.Printf("  URL: %s\n", spec.Source.URL)
			os.Exit(1)
		}

		fmt.Printf("Fetching %s...\n", spec.ID)
		if err := fetchSpec(baseDir, *spec); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Success!")
	}
}

func fetchSpec(baseDir string, spec SpecConfig) error {
	// Create output directory relative to baseDir
	outputDir := filepath.Join(baseDir, spec.Output.Path)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Process each step
	for _, step := range spec.Processing {
		if err := processStep(spec, step, outputDir); err != nil {
			return fmt.Errorf("processing step %s failed: %w", step.Type, err)
		}
	}

	return nil
}

func processStep(spec SpecConfig, step ProcessingStep, outputDir string) error {
	switch step.Type {
	case "download":
		outputFile := step.Output
		if outputFile == "" {
			outputFile = spec.Output.Filename
		}
		return downloadFile(spec.Source.URL, filepath.Join(outputDir, outputFile))

	case "convert":
		inputPath := filepath.Join(outputDir, step.Input)
		outputPath := filepath.Join(outputDir, step.Output)
		return convertSpec(inputPath, outputPath, step.From, step.To, step.Format)

	case "sed-replace":
		filePath := filepath.Join(outputDir, spec.Output.Filename)
		return sedReplace(filePath, step.Pattern, step.Replacement)

	case "unwrap-single-anyof":
		filePath := filepath.Join(outputDir, spec.Output.Filename)
		return unwrapSingleAnyOf(filePath)

	case "manual":
		// Skip manual steps
		return nil

	default:
		return fmt.Errorf("unknown processing type: %s", step.Type)
	}
}

func downloadFile(url, filepath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %s", resp.Status)
	}

	if strings.HasSuffix(strings.ToLower(filepath), ".json") {
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read download body: %w", err)
		}
		formatted, err := formatJSONBytes(data)
		if err != nil {
			return fmt.Errorf("downloaded file is not valid JSON: %w", err)
		}
		if err := os.WriteFile(filepath, formatted, 0644); err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}
		return nil
	}

	out, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func convertSpec(inputPath, outputPath, from, to, format string) error {
	// Read input file
	inputData, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("failed to read input file: %w", err)
	}

	// Parse JSON input (assuming swagger/openapi 2.0 input)
	var spec map[string]interface{}
	if err := json.Unmarshal(inputData, &spec); err != nil {
		return fmt.Errorf("failed to parse input JSON: %w", err)
	}

	// Basic conversion from Swagger 2.0 to OpenAPI 3.0
	if from == "swagger2" && to == "openapi3" {
		spec = convertSwagger2ToOpenAPI3(spec)
	}

	// Write output
	var outputData []byte
	if format == "yaml" {
		outputData, err = yaml.Marshal(spec)
	} else {
		outputData, err = json.MarshalIndent(spec, "", "  ")
	}
	if err != nil {
		return fmt.Errorf("failed to marshal output: %w", err)
	}

	if err := os.WriteFile(outputPath, outputData, 0644); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	return nil
}

func convertSwagger2ToOpenAPI3(swagger map[string]interface{}) map[string]interface{} {
	// Basic conversion - covers common fields
	openapi := make(map[string]interface{})

	// Set OpenAPI version
	openapi["openapi"] = "3.0.0"

	// Copy info
	if info, ok := swagger["info"]; ok {
		openapi["info"] = info
	}

	// Convert servers from host/basePath/schemes
	servers := make([]map[string]interface{}, 0)
	host := ""
	basePath := ""
	schemes := []interface{}{"https"}

	if h, ok := swagger["host"].(string); ok {
		host = h
	}
	if bp, ok := swagger["basePath"].(string); ok {
		basePath = bp
	}
	if s, ok := swagger["schemes"].([]interface{}); ok && len(s) > 0 {
		schemes = s
	}

	for _, scheme := range schemes {
		server := map[string]interface{}{
			"url": fmt.Sprintf("%s://%s%s", scheme, host, basePath),
		}
		servers = append(servers, server)
	}
	openapi["servers"] = servers

	// Copy paths (will update refs later)
	if paths, ok := swagger["paths"]; ok {
		openapi["paths"] = paths
	}

	// Copy tags
	if tags, ok := swagger["tags"]; ok {
		openapi["tags"] = tags
	}

	// Move definitions to components/schemas
	if definitions, ok := swagger["definitions"]; ok {
		components := map[string]interface{}{
			"schemas": definitions,
		}
		openapi["components"] = components
	}

	// Update all $ref pointers from #/definitions/ to #/components/schemas/
	updateRefs(openapi)

	// Convert Swagger 2.0 "in": "body" parameters into OpenAPI 3.0
	// requestBody objects (must run before convertParameters below).
	convertRequestBodies(openapi)

	// Convert Swagger 2.0 parameters to OpenAPI 3.0 format
	convertParameters(openapi)

	return openapi
}

// convertRequestBodies converts Swagger 2.0 "in": "body" operation
// parameters into OpenAPI 3.0 requestBody objects. Swagger 2.0 allows at
// most one body parameter per operation; its "schema" becomes the
// requestBody's application/json schema and its "required" flag carries
// over directly.
func convertRequestBodies(openapi map[string]interface{}) {
	paths, ok := openapi["paths"].(map[string]interface{})
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
			params, ok := operation["parameters"].([]interface{})
			if !ok {
				continue
			}
			remaining := make([]interface{}, 0, len(params))
			for _, paramValue := range params {
				param, ok := paramValue.(map[string]interface{})
				if !ok {
					remaining = append(remaining, paramValue)
					continue
				}
				if in, _ := param["in"].(string); in == "body" {
					required, _ := param["required"].(bool)
					operation["requestBody"] = map[string]interface{}{
						"required": required,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": param["schema"],
							},
						},
					}
					continue
				}
				remaining = append(remaining, paramValue)
			}
			if len(remaining) > 0 {
				operation["parameters"] = remaining
			} else {
				delete(operation, "parameters")
			}
		}
	}
}

// updateRefs recursively updates all $ref pointers from Swagger 2.0 to OpenAPI 3.0 format
func updateRefs(obj interface{}) {
	switch v := obj.(type) {
	case map[string]interface{}:
		for key, value := range v {
			if key == "$ref" {
				if ref, ok := value.(string); ok {
					// Replace #/definitions/ with #/components/schemas/
					if strings.HasPrefix(ref, "#/definitions/") {
						v[key] = strings.Replace(ref, "#/definitions/", "#/components/schemas/", 1)
					}
				}
			} else {
				updateRefs(value)
			}
		}
	case []interface{}:
		for _, item := range v {
			updateRefs(item)
		}
	}
}

// convertParameters recursively converts Swagger 2.0 parameters to OpenAPI 3.0 format
// In Swagger 2.0, parameters have type, format, etc. directly on the parameter object
// In OpenAPI 3.0, these must be wrapped in a schema object
func convertParameters(obj interface{}) {
	switch v := obj.(type) {
	case map[string]interface{}:
		// Check if this is a parameter object
		if _, hasName := v["name"]; hasName {
			if _, hasIn := v["in"]; hasIn {
				// This looks like a parameter - check if it needs conversion
				if _, hasType := v["type"]; hasType {
					// Has type directly - needs conversion to schema
					schema := make(map[string]interface{})

					// Move type-related fields to schema
					typeFields := []string{"type", "format", "items", "minimum", "maximum",
						"minLength", "maxLength", "pattern", "enum", "default"}
					for _, field := range typeFields {
						if val, ok := v[field]; ok {
							schema[field] = val
							delete(v, field)
						}
					}

					// Only add schema if we moved something
					if len(schema) > 0 {
						v["schema"] = schema
					}
				}

				// Continue recursing through remaining fields
				for key, value := range v {
					if key != "name" && key != "in" && key != "schema" {
						convertParameters(value)
					}
				}
			}
		} else {
			// Not a parameter, recurse through all fields
			for _, value := range v {
				convertParameters(value)
			}
		}
	case []interface{}:
		for _, item := range v {
			convertParameters(item)
		}
	}
}

func sedReplace(filepath, pattern, replacement string) error {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	content := string(data)
	newContent := strings.ReplaceAll(content, pattern, replacement)

	if err := os.WriteFile(filepath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// unwrapSingleAnyOf walks the JSON tree and collapses any object of the form
//
//	{"anyOf": [X], ...rest}
//
// into a merged object that combines X's fields with rest (rest fields win on
// conflict so that user-facing metadata like description/title/default is kept).
// This is intended for OpenAPI 3.1 specs that use `anyOf: [X, {type: null}]`
// for nullable fields: after stripping the null variant via sed-replace the
// singleton anyOf wrapper is meaningless but trips up oapi-codegen which
// generates index-suffixed types per anyOf variant and can collide on title.
func unwrapSingleAnyOf(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	var doc interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	transformed := walkUnwrapAnyOf(doc)

	out, err := json.Marshal(transformed)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	out, err = formatJSONBytes(out)
	if err != nil {
		return fmt.Errorf("failed to format JSON: %w", err)
	}

	if err := os.WriteFile(filePath, out, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}

func formatJSONBytes(data []byte) ([]byte, error) {
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, data, "", "  "); err != nil {
		return nil, err
	}
	return formatted.Bytes(), nil
}

func walkUnwrapAnyOf(node interface{}) interface{} {
	switch v := node.(type) {
	case map[string]interface{}:
		// First, recurse into children.
		for k, child := range v {
			v[k] = walkUnwrapAnyOf(child)
		}
		// Then, collapse singleton anyOf at this level.
		raw, ok := v["anyOf"]
		if !ok {
			return v
		}
		arr, ok := raw.([]interface{})
		if !ok || len(arr) != 1 {
			return v
		}
		inner, ok := arr[0].(map[string]interface{})
		if !ok {
			return v
		}
		delete(v, "anyOf")
		// Merge inner into v; do NOT overwrite parent metadata
		// (description, title, default, examples, etc.).
		for ik, iv := range inner {
			if _, exists := v[ik]; !exists {
				v[ik] = iv
			}
		}
		return v
	case []interface{}:
		for i, child := range v {
			v[i] = walkUnwrapAnyOf(child)
		}
		return v
	default:
		return v
	}
}

func handleList(baseDir string) {
	config, err := loadConfig(baseDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Available OpenAPI Specifications:")
	fmt.Println()

	for _, spec := range config.Specs {
		fmt.Printf("%-35s %s\n", spec.ID, spec.Name)
		fmt.Printf("  ├─ Vendor:  %s\n", spec.Vendor)
		fmt.Printf("  ├─ Service: %s\n", spec.Service)
		if spec.Version != "" {
			fmt.Printf("  ├─ Version: %s\n", spec.Version)
		}
		fmt.Printf("  ├─ Type:    %s\n", spec.Source.Type)
		fmt.Printf("  └─ Output:  %s/%s\n", spec.Output.Path, spec.Output.Filename)
		fmt.Println()
	}
}

func handleInfo(baseDir string, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Error: spec-id required")
		printUsage()
		os.Exit(1)
	}

	config, err := loadConfig(baseDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	spec := findSpec(config, args[0])
	if spec == nil {
		fmt.Fprintf(os.Stderr, "Error: spec not found: %s\n", args[0])
		os.Exit(1)
	}

	fmt.Printf("Specification: %s\n", spec.Name)
	fmt.Printf("ID:            %s\n", spec.ID)
	fmt.Printf("Vendor:        %s\n", spec.Vendor)
	fmt.Printf("Service:       %s\n", spec.Service)
	if spec.Version != "" {
		fmt.Printf("Version:       %s\n", spec.Version)
	}
	fmt.Printf("Format:        %s\n", spec.Format)
	fmt.Printf("\nSource:\n")
	fmt.Printf("  Type: %s\n", spec.Source.Type)
	fmt.Printf("  URL:  %s\n", spec.Source.URL)
	if spec.Source.Description != "" {
		fmt.Printf("  Note: %s\n", spec.Source.Description)
	}
	fmt.Printf("\nOutput:\n")
	fmt.Printf("  Path: %s\n", spec.Output.Path)
	fmt.Printf("  File: %s\n", spec.Output.Filename)

	if len(spec.Processing) > 0 {
		fmt.Printf("\nProcessing Steps:\n")
		for i, step := range spec.Processing {
			fmt.Printf("  %d. %s\n", i+1, step.Type)
			if step.Reason != "" {
				fmt.Printf("     Reason: %s\n", step.Reason)
			}
			if step.Notes != "" {
				fmt.Printf("     Notes: %s\n", step.Notes)
			}
		}
	}
}

func handleValidate(baseDir string, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Error: spec-id or 'all' required")
		printUsage()
		os.Exit(1)
	}

	config, err := loadConfig(baseDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	target := args[0]
	if target == "unified" {
		path := filepath.Join(baseDir, unifiedSpecRelativePath)
		if err := validateOpenAPIDocument(path); err != nil {
			fmt.Printf("✗ unified invalid: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ unified spec valid\n")
		return
	}

	if target == "all" {
		allValid := true
		for _, spec := range config.Specs {
			exists := validateSpec(baseDir, spec)
			status := "✓"
			if !exists {
				status = "✗"
				allValid = false
			}
			fmt.Printf("%s %s\n", status, spec.ID)
		}
		if !allValid {
			os.Exit(1)
		}
	} else {
		spec := findSpec(config, target)
		if spec == nil {
			fmt.Fprintf(os.Stderr, "Error: spec not found: %s\n", target)
			os.Exit(1)
		}

		if validateSpec(baseDir, *spec) {
			fmt.Printf("✓ %s exists\n", spec.ID)
		} else {
			fmt.Printf("✗ %s not found\n", spec.ID)
			os.Exit(1)
		}
	}
}

func validateSpec(baseDir string, spec SpecConfig) bool {
	filePath := filepath.Join(baseDir, spec.Output.Path, spec.Output.Filename)
	_, err := os.Stat(filePath)
	return err == nil
}

func findSpec(config *Config, id string) *SpecConfig {
	for _, spec := range config.Specs {
		if spec.ID == id {
			return &spec
		}
	}
	return nil
}
