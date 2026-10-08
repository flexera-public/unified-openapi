package main

import (
	"fmt"
	"strings"
)

// SchemaFormatRemoval drops a `format` from one component schema property
// whose live values do not honour it (e.g. empty strings or sentinels in a
// date-time field), so strictly typed clients can still decode responses.
type SchemaFormatRemoval struct {
	Schema   string `yaml:"schema"`
	Property string `yaml:"property"`
	Reason   string `yaml:"reason"`
}

func applySchemaFormatRemovals(doc map[string]interface{}, specs []SpecConfig) error {
	components, _ := doc["components"].(map[string]interface{})
	schemas, _ := components["schemas"].(map[string]interface{})
	seen := make(map[string]string)
	for _, spec := range specs {
		for _, removal := range spec.SchemaFormatRemovals {
			name := strings.TrimSpace(removal.Schema)
			property := strings.TrimSpace(removal.Property)
			if name == "" || property == "" || strings.TrimSpace(removal.Reason) == "" {
				return fmt.Errorf("spec %s schema format removal must specify schema, property, and reason", spec.ID)
			}
			key := name + "." + property
			if previous, exists := seen[key]; exists {
				return fmt.Errorf("duplicate schema format removal %s in specs %s and %s", key, previous, spec.ID)
			}
			seen[key] = spec.ID
			schema, _ := schemas[name].(map[string]interface{})
			properties, _ := schema["properties"].(map[string]interface{})
			prop, _ := properties[property].(map[string]interface{})
			if prop == nil {
				return fmt.Errorf("spec %s schema format removal does not match %s", spec.ID, key)
			}
			if _, has := prop["format"]; !has {
				return fmt.Errorf("spec %s schema format removal %s has no format; remove the override", spec.ID, key)
			}
			delete(prop, "format")
		}
	}
	return nil
}
