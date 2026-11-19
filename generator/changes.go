package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type ChangeRecord struct {
	Path          string `json:"path"`
	Action        string `json:"action"`
	VersionImpact string `json:"version_impact"`
	Category      string `json:"category,omitempty"`
}
type FileChange struct {
	File          string         `json:"file"`
	Status        string         `json:"status"`
	Changes       []ChangeRecord `json:"changes,omitempty"`
	VersionImpact string         `json:"version_impact"`
	fingerprint   [32]byte
}
type ChangeReport struct {
	HasFunctionalChanges bool         `json:"has_functional_changes"`
	Files                []FileChange `json:"files"`
	VersionImpact        string       `json:"version_impact"`
}
type exactNumber string

// Decode through nodes to reject duplicate keys and retain exact numeric values.
func parseChangeDocument(name string, data []byte) (any, error) {
	if strings.HasSuffix(name, ".json") && !json.Valid(data) {
		return nil, fmt.Errorf("%s: invalid JSON", name)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var node yaml.Node
	if err := dec.Decode(&node); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return nil, fmt.Errorf("%s: multiple documents are unsupported", name)
	} else if err != io.EOF {
		return nil, err
	}
	value, err := changeNode(&node)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected an OpenAPI object", name)
	}
	if _, ok := root["openapi"]; !ok {
		if root["swagger"] != "2.0" {
			return nil, fmt.Errorf("%s: unsupported specification version", name)
		}
	} else {
		version, ok := root["openapi"].(string)
		if !ok || !strings.HasPrefix(version, "3.") {
			return nil, fmt.Errorf("%s: unsupported OpenAPI version", name)
		}
	}
	return root, nil
}
func changeNode(n *yaml.Node) (any, error) {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) != 1 {
			return nil, fmt.Errorf("empty document")
		}
		return changeNode(n.Content[0])
	case yaml.MappingNode:
		out := map[string]any{}
		for i := 0; i < len(n.Content); i += 2 {
			key := n.Content[i]
			// Unquoted numeric HTTP response keys are common in valid YAML specs.
			if key.Kind != yaml.ScalarNode || (key.Tag != "!!str" && key.Tag != "!!int") {
				return nil, fmt.Errorf("unsupported mapping key")
			}
			if _, exists := out[key.Value]; exists {
				return nil, fmt.Errorf("duplicate key %q", key.Value)
			}
			v, err := changeNode(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			out[key.Value] = v
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]any, len(n.Content))
		for i, c := range n.Content {
			v, err := changeNode(c)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!str":
			return n.Value, nil
		case "!!null":
			return nil, nil
		case "!!bool":
			return strings.EqualFold(n.Value, "true"), nil
		case "!!int", "!!float":
			text := strings.ReplaceAll(n.Value, "_", "")
			r, ok := new(big.Rat).SetString(text)
			if !ok {
				return nil, fmt.Errorf("unsupported number %q", n.Value)
			}
			return exactNumber(r.RatString()), nil
		default:
			return nil, fmt.Errorf("unsupported YAML tag %s", n.Tag)
		}
	default:
		return nil, fmt.Errorf("aliases and unsupported YAML nodes are not allowed")
	}
}

// Roles distinguish specification metadata from user-defined dictionary keys,
// schema properties, extension payloads, defaults, and enums.
func childChangeRole(role, key string) string {
	if strings.HasPrefix(role, "dict:") {
		return strings.TrimPrefix(role, "dict:")
	}
	if strings.HasPrefix(key, "x-") {
		return "opaque"
	}
	switch role {
	case "root":
		switch key {
		case "info":
			return "info"
		case "tags":
			return "tag"
		case "securityDefinitions":
			return "dict:securityScheme"
		case "paths":
			return "dict:path"
		case "components":
			return "components"
		case "definitions":
			return "dict:schema"
		case "parameters":
			return "dict:parameter"
		case "responses":
			return "dict:response"
		}
	case "components":
		switch key {
		case "schemas":
			return "dict:schema"
		case "securitySchemes":
			return "dict:securityScheme"
		case "parameters":
			return "dict:parameter"
		case "headers":
			return "dict:header"
		case "responses":
			return "dict:response"
		case "requestBodies":
			return "dict:requestBody"
		case "callbacks":
			return "dict:callback"
		case "pathItems":
			return "dict:path"
		}
	case "callback":
		return "path"
	case "path":
		for _, method := range operationMethods {
			if key == method {
				return "operation"
			}
		}
		if key == "parameters" {
			return "parameter"
		}
	case "operation":
		switch key {
		case "parameters":
			return "parameter"
		case "responses":
			return "dict:response"
		case "requestBody":
			return "requestBody"
		case "callbacks":
			return "dict:callback"
		}
	case "schema":
		switch key {
		case "properties", "patternProperties", "dependentSchemas", "$defs", "definitions":
			return "dict:schema"
		case "items", "additionalProperties", "allOf", "anyOf", "oneOf", "not", "if", "then", "else", "contains", "propertyNames", "unevaluatedProperties", "unevaluatedItems", "prefixItems":
			return "schema"
		}
	case "parameter", "header":
		switch key {
		case "schema", "items":
			return "schema"
		case "content":
			return "dict:media"
		}
	case "response":
		switch key {
		case "schema":
			return "schema"
		case "headers":
			return "dict:header"
		case "content":
			return "dict:media"
		}
	case "requestBody":
		if key == "content" {
			return "dict:media"
		}
	case "media":
		if key == "schema" {
			return "schema"
		}
	}
	return "opaque"
}
func isExampleMetadata(role, key string) bool {
	if role == "components" {
		return key == "examples"
	}
	switch role {
	case "schema", "parameter", "header", "media":
		return key == "example" || key == "examples"
	case "response":
		return key == "examples"
	}
	return false
}
func withoutExamples(v any, role string) any {
	switch v := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, c := range v {
			if isExampleMetadata(role, k) {
				continue
			}
			next := childChangeRole(role, k)
			filtered := withoutExamples(c, next)
			if next == "components" {
				if m, ok := filtered.(map[string]any); ok && len(m) == 0 {
					if original, ok := c.(map[string]any); ok && len(original) > 0 {
						continue
					}
				}
			}
			out[k] = filtered
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, c := range v {
			out[i] = withoutExamples(c, role)
		}
		return out
	default:
		return v
	}
}
func pointerKey(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}
func functionalDiff(before, after any, path string, out *[]ChangeRecord) {
	functionalDiffRole(before, after, path, "root", out)
}

func functionalDiffRole(before, after any, path, role string, out *[]ChangeRecord) {
	if reflect.DeepEqual(before, after) {
		return
	}
	b, bok := before.(map[string]any)
	a, aok := after.(map[string]any)
	if bok && aok {
		keys := map[string]bool{}
		for k := range b {
			keys[k] = true
		}
		for k := range a {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			bv, be := b[k]
			av, ae := a[k]
			p := path + "/" + pointerKey(k)
			action := "modified"
			if !be {
				action = "added"
			} else if !ae {
				action = "removed"
			}
			if !be || !ae || changeMetadataCategory(role, k) != "" || (role == "schema" && (k == "required" || k == "enum")) {
				*out = append(*out, classifyChange(p, action, role, k, bv, av))
			} else {
				functionalDiffRole(bv, av, p, childChangeRole(role, k), out)
			}
		}
		return
	}
	ba, bok := before.([]any)
	aa, aok := after.([]any)
	if bok && aok && len(ba) == len(aa) {
		for i := range ba {
			functionalDiffRole(ba[i], aa[i], path+"/"+strconv.Itoa(i), role, out)
		}
		return
	}
	if path == "" {
		path = "/"
	}
	*out = append(*out, classifyChange(path, "modified", "opaque", "", before, after))
}
func compareSpecBytes(name string, before, after []byte) (FileChange, error) {
	result := FileChange{File: name, Status: "unchanged", VersionImpact: "none"}
	if bytes.Equal(before, after) {
		return result, nil
	}
	var b, a any
	var err error
	if before != nil {
		b, err = parseChangeDocument(name, before)
		if err != nil {
			return result, err
		}
	}
	if after != nil {
		a, err = parseChangeDocument(name, after)
		if err != nil {
			return result, err
		}
	}
	if before == nil || after == nil {
		action := "added"
		if after == nil {
			action = "removed"
		}
		result.Status = "functional"
		result.Changes = []ChangeRecord{classifyChange("/", action, "root", "", b, a)}
		result.VersionImpact = fileVersionImpact(result)
		return result, nil
	}
	if reflect.DeepEqual(b, a) {
		result.Status = "formatting-only"
		return result, nil
	}
	filteredBefore, filteredAfter := withoutExamples(b, "root"), withoutExamples(a, "root")
	functionalDiff(filteredBefore, filteredAfter, "", &result.Changes)
	result.Status = "examples-only"
	if len(result.Changes) > 0 {
		result.Status = "functional"
		// Compare companion values as well as locations, without exposing payloads.
		canonical, err := json.Marshal([]any{filteredBefore, filteredAfter})
		if err != nil {
			return result, err
		}
		result.fingerprint = sha256.Sum256(canonical)
	}
	result.VersionImpact = fileVersionImpact(result)
	return result, nil
}

func renderChangeReport(report ChangeReport, markdown bool) string {
	var out strings.Builder
	fmt.Fprintf(&out, "Suggested version bump: %s (conservative heuristic; review required).\n\n", reportVersionImpact(report))
	if !report.HasFunctionalChanges {
		out.WriteString("No functional changes detected.\n")
		return out.String()
	}
	if markdown {
		out.WriteString("### Functional changes\n\n")
	} else {
		out.WriteString("Functional changes:\n")
	}
	count, omitted := 0, 0
	var jsonFingerprint [32]byte
	for _, f := range report.Files {
		if f.File == "openapi3.json" {
			jsonFingerprint = f.fingerprint
		}
	}
	for _, f := range report.Files {
		if f.Status != "functional" {
			continue
		}
		if f.File == "openapi3.yaml" && jsonFingerprint != ([32]byte{}) && f.fingerprint == jsonFingerprint {
			continue
		}
		var descriptions int
		for _, c := range f.Changes {
			if c.Category == "description" {
				descriptions++
			}
		}
		wroteHeader := false
		writeHeader := func() {
			if wroteHeader {
				return
			}
			if markdown {
				fmt.Fprintf(&out, "#### %s\n\n", reportLabel(f.File))
			} else {
				fmt.Fprintf(&out, "%s\n", strconv.Quote(f.File))
			}
			wroteHeader = true
		}
		// Documentation gets one low-significance line, regardless of volume.
		if descriptions > 0 {
			writeHeader()
			fmt.Fprintf(&out, "- patch: %d description changes (nonbreaking; consolidated).\n", descriptions)
		}
		for _, c := range f.Changes {
			if c.Category == "description" {
				continue
			}
			if count >= 100 {
				omitted++
				continue
			}
			writeHeader()
			path := humanChangePath(c.Path)
			if markdown {
				fmt.Fprintf(&out, "- %s: %s %s\n", c.VersionImpact, c.Action, reportLabel(path))
			} else {
				fmt.Fprintf(&out, "  %s: %s %s\n", c.VersionImpact, c.Action, strconv.Quote(path))
			}
			count++
		}
		if wroteHeader && markdown {
			out.WriteByte('\n')
		}
	}
	if omitted > 0 {
		fmt.Fprintf(&out, "%d additional functional changes omitted; use JSON output for the full report.\n", omitted)
	}
	return out.String()
}

func humanChangePath(pointer string) string {
	if pointer == "/" {
		return "(document)"
	}
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	for i, part := range parts {
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
	}
	return strings.Join(parts, " → ")
}

func reportLabel(s string) string {
	// Code spans protect tildes, HTML, and other Markdown metacharacters.
	// A delimiter longer than every embedded backtick run cannot be closed by it.
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, s)
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	delimiter := strings.Repeat("`", longest+1)
	return delimiter + " " + s + " " + delimiter
}
