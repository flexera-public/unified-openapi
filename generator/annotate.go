package main

import (
	"regexp"
	"strings"
)

// annotateForCLI walks the unified spec and adds three custom extensions to
// every operation, intended for consumption by spec-driven CLI/SDK
// codegen (Phase 3+):
//
//	x-flexera-resource: kebab-case resource name derived from the path
//	                    (e.g. "budget", "bill-connect-aws-iam-role").
//	x-flexera-action:   one of list, get, create, update, replace,
//	                    delete, action — derived from method + path
//	                    shape.
//	x-flexera-paginated: true on operations whose 2xx JSON response
//	                     schema contains both `nextPage` and `values`.
//	x-flexera-list-item-ref: $ref of the values[] item schema when the
//	                         paginated response references a named
//	                         component (lets codegen pick the typed
//	                         element type).
//
// The function is idempotent: running it twice yields the same document.
func annotateForCLI(doc map[string]interface{}) {
	paths, ok := doc["paths"].(map[string]interface{})
	if !ok {
		return
	}
	components, _ := doc["components"].(map[string]interface{})
	schemas := map[string]interface{}{}
	if components != nil {
		if s, ok := components["schemas"].(map[string]interface{}); ok {
			schemas = s
		}
	}

	for pathKey, pathItemValue := range paths {
		pathItem, ok := pathItemValue.(map[string]interface{})
		if !ok {
			continue
		}
		resource := resourceFromPath(pathKey)
		for _, method := range operationMethods {
			op, ok := pathItem[method].(map[string]interface{})
			if !ok {
				continue
			}
			if resource != "" {
				op["x-flexera-resource"] = resource
			}
			op["x-flexera-action"] = actionFromOperation(method, pathKey)

			ref, paginated := paginatedItemRef(op, schemas)
			if paginated {
				op["x-flexera-paginated"] = true
				if ref != "" {
					op["x-flexera-list-item-ref"] = ref
				}
			}
		}
	}
}

// pathVarRE matches OpenAPI path template segments like {orgId}.
var pathVarRE = regexp.MustCompile(`^\{[^}]+\}$`)

// resourceFromPath returns the kebab-case "resource" name for an OpenAPI
// path. It drops the leading service segment (e.g. "finops-onboarding"),
// version segment (e.g. "v1"), org/project scoping segments
// ("orgs/{orgId}", "projects/{projectId}"), and any trailing
// ID/parameter/sub-action segments, then joins the remaining literal
// segments with hyphens.
//
// Examples:
//
//	/budget/v1/orgs/{orgId}/budgets                                 -> "budget"
//	/finops-onboarding/v1/orgs/{orgId}/bill-connects/aws/iam-role   -> "bill-connect-aws-iam-role"
//	/iam/v1/orgs/{orgId}/users/{userId}                             -> "user"
//	/policy/v1/orgs/{orgId}/projects/{projectId}/applied-policies/{id}/evaluate -> "applied-policy-evaluate"
//	/grs/orgs/{orgId}/projects                                      -> "project"
func resourceFromPath(p string) string {
	// Rule-based-dimension rules are modeled as dated "rules lists"
	// under /rule-based-dimensions/{id}/rules/{effectiveAt}. Preserve
	// the list identity in the generated resource label so Terraform
	// emits `rule_based_dimension_rules_list`.
	if strings.Contains(p, "/rule-based-dimensions/{id}/rules/{effectiveAt}") {
		return "rule-based-dimension-rules-list"
	}
	segs := strings.Split(strings.Trim(p, "/"), "/")
	if len(segs) == 0 {
		return ""
	}
	out := []string{}
	skipNext := false
	for i, s := range segs {
		if skipNext {
			skipNext = false
			continue
		}
		// Drop path variables.
		if pathVarRE.MatchString(s) {
			continue
		}
		// Drop leading service + version segments.
		if i == 0 {
			continue
		}
		if i == 1 && (s == "v1" || s == "v2" || s == "v3") {
			continue
		}
		// Drop scoping pairs: orgs/{orgId}, projects/{projectId}, users/{userId}.
		if s == "orgs" || s == "projects" || s == "users" {
			// Only drop when followed by a path variable.
			if i+1 < len(segs) && pathVarRE.MatchString(segs[i+1]) {
				skipNext = true
				continue
			}
		}
		out = append(out, s)
	}
	// Singularize trailing "s" only for obvious plurals (>=4 chars,
	// not already singular like "status"). Keep multi-segment trailing
	// sub-actions ("evaluate", "log") intact.
	if len(out) == 0 {
		// fall back to the segment before the trailing variables
		for i := len(segs) - 1; i >= 0; i-- {
			if !pathVarRE.MatchString(segs[i]) {
				return singularize(segs[i])
			}
		}
		return ""
	}
	// Singularize the *first* output segment (the primary resource).
	out[0] = singularize(out[0])
	return strings.Join(out, "-")
}

func singularize(s string) string {
	if len(s) <= 3 {
		return s
	}
	switch {
	case strings.HasSuffix(s, "ies"):
		return s[:len(s)-3] + "y"
	case strings.HasSuffix(s, "ses") || strings.HasSuffix(s, "xes"):
		return s[:len(s)-2]
	case strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss") && !strings.HasSuffix(s, "us") && !strings.HasSuffix(s, "is"):
		return s[:len(s)-1]
	}
	return s
}

// actionFromOperation derives a coarse-grained CLI action verb from the
// HTTP method and path shape.
func actionFromOperation(method, path string) string {
	method = strings.ToLower(method)
	segs := strings.Split(strings.Trim(path, "/"), "/")
	endsWithID := len(segs) > 0 && pathVarRE.MatchString(segs[len(segs)-1])

	switch method {
	case "get":
		if endsWithID {
			return "get"
		}
		return "list"
	case "post":
		// POST on a resource itself is a custom action.
		if endsWithID {
			return "action"
		}
		// POST on /parent/{id}/sub-action — last seg literal,
		// second-last seg path variable. This is a custom action
		// UNLESS the variable is a scoping segment (orgs/{orgId},
		// projects/{projectId}, users/{userId}) in which case the
		// trailing literal is just the collection.
		if len(segs) >= 3 && pathVarRE.MatchString(segs[len(segs)-2]) {
			scopingParent := segs[len(segs)-3]
			if scopingParent != "orgs" && scopingParent != "projects" && scopingParent != "users" {
				return "action"
			}
		}
		return "create"
	case "put":
		return "replace"
	case "patch":
		return "update"
	case "delete":
		return "delete"
	default:
		return method
	}
}

// paginatedItemRef inspects an operation's 200/201 response schema for
// the standard Flexera pagination envelope (`nextPage` + `values`). When
// found, it returns the $ref of the values[] item schema (if available)
// and paginated=true.
func paginatedItemRef(op map[string]interface{}, schemas map[string]interface{}) (string, bool) {
	responses, ok := op["responses"].(map[string]interface{})
	if !ok {
		return "", false
	}
	for _, code := range []string{"200", "201"} {
		resp, ok := responses[code].(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := resp["content"].(map[string]interface{})
		if !ok {
			continue
		}
		for mt, mtVal := range content {
			if !strings.Contains(mt, "json") {
				continue
			}
			mtMap, ok := mtVal.(map[string]interface{})
			if !ok {
				continue
			}
			schema, ok := mtMap["schema"].(map[string]interface{})
			if !ok {
				continue
			}
			ref, paginated := schemaPaginated(schema, schemas, map[string]bool{})
			if paginated {
				return ref, true
			}
		}
	}
	return "", false
}

// schemaPaginated returns (itemRef, true) when schema (possibly via $ref
// chasing) is a Flexera-pagination-envelope object: has `nextPage` and
// `values` properties.
func schemaPaginated(schema map[string]interface{}, schemas map[string]interface{}, seen map[string]bool) (string, bool) {
	if schema == nil {
		return "", false
	}
	if ref, ok := schema["$ref"].(string); ok {
		if seen[ref] {
			return "", false
		}
		seen[ref] = true
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		if target, ok := schemas[name].(map[string]interface{}); ok {
			return schemaPaginated(target, schemas, seen)
		}
		return "", false
	}
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		return "", false
	}
	if _, hasNext := props["nextPage"]; !hasNext {
		return "", false
	}
	values, hasValues := props["values"].(map[string]interface{})
	if !hasValues {
		return "", false
	}
	// values is typically {type: array, items: {$ref: ...}}.
	if items, ok := values["items"].(map[string]interface{}); ok {
		if ref, ok := items["$ref"].(string); ok {
			return ref, true
		}
	}
	return "", true
}
