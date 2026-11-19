package main

import "reflect"

// Metadata is recognized by specification role, never by an arbitrary key name.
func changeMetadataCategory(role, key string) string {
	switch role {
	case "info":
		if key == "version" {
			return "version"
		}
		if key == "description" {
			return "description"
		}
	case "schema", "parameter", "header", "response", "requestBody", "media", "path", "operation", "tag", "securityScheme":
		if key == "description" {
			return "description"
		}
	}
	return ""
}

func classifyChange(path, action, role, key string, before, after any) ChangeRecord {
	c := ChangeRecord{Path: path, Action: action, VersionImpact: "major", Category: changeMetadataCategory(role, key)}
	if c.Category != "" {
		c.VersionImpact = "patch"
		return c
	}
	if action == "removed" {
		return c
	}
	if action == "added" {
		c.VersionImpact = "minor"
		if addedRequiredInput(after, childChangeRole(role, key)) {
			c.VersionImpact = "major"
		}
	}
	// New constraints may reject inputs previously accepted. Conservatively
	// flag them even when the surrounding request/response direction is unknown.
	if role == "schema" {
		switch key {
		case "required":
			c.VersionImpact = "minor"
			if action == "added" || !arraySubset(after, before) {
				c.VersionImpact = "major"
			}
		case "enum":
			c.VersionImpact = "major"
			if action == "removed" || arraySubset(before, after) {
				c.VersionImpact = "minor"
			}
		case "type", "$ref", "format", "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "minLength", "maxLength", "minItems", "maxItems", "minProperties", "maxProperties", "pattern", "multipleOf", "additionalProperties", "unevaluatedProperties", "uniqueItems", "nullable", "const", "allOf", "anyOf", "oneOf", "not":
			c.VersionImpact = "major"
		}
	}
	if (role == "parameter" || role == "requestBody" || role == "header") && key == "required" {
		c.VersionImpact = "major"
	}
	if key == "security" && (role == "root" || role == "operation") {
		c.VersionImpact = "major"
	}
	return c
}

func addedRequiredInput(value any, role string) bool {
	switch v := value.(type) {
	case map[string]any:
		if role == "parameter" || role == "requestBody" || role == "header" {
			return v["required"] == true
		}
		if role == "dict:parameter" {
			for _, item := range v {
				if addedRequiredInput(item, "parameter") {
					return true
				}
			}
		}
	case []any:
		for _, item := range v {
			if addedRequiredInput(item, role) {
				return true
			}
		}
	}
	return false
}

func arraySubset(left, right any) bool {
	if left == nil {
		return true
	}
	l, lok := left.([]any)
	r, rok := right.([]any)
	if !lok || !rok {
		return false
	}
	for _, v := range l {
		found := false
		for _, candidate := range r {
			if reflect.DeepEqual(v, candidate) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func strongestImpact(left, right string) string {
	rank := map[string]int{"": 0, "none": 0, "patch": 1, "minor": 2, "major": 3}
	if rank[right] > rank[left] {
		return right
	}
	if left == "" {
		return "none"
	}
	return left
}

func fileVersionImpact(file FileChange) string {
	impact := "none"
	if file.Status == "examples-only" {
		impact = "patch"
	}
	for _, c := range file.Changes {
		impact = strongestImpact(impact, c.VersionImpact)
	}
	return impact
}

func reportVersionImpact(report ChangeReport) string {
	impact := "none"
	for _, file := range report.Files {
		impact = strongestImpact(impact, fileVersionImpact(file))
	}
	return impact
}
