package main

import "testing"

func TestResourceFromPath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/budget/v1/orgs/{orgId}/budgets", "budget"},
		{"/finops-onboarding/v1/orgs/{orgId}/bill-connects/aws/iam-role", "bill-connect-aws-iam-role"},
		{"/iam/v1/orgs/{orgId}/users/{userId}", "user"},
		{"/grs/orgs/{orgId}/projects", "project"},
		{"/policy/v1/orgs/{orgId}/projects/{projectId}/applied-policies/{id}/evaluate", "applied-policy-evaluate"},
		{"/cred/v2/orgs/{orgId}/credentials/api-key/{id}", "credential-api-key"},
		{"/finops-customizations/v1/orgs/{orgId}/rule-based-dimensions/{id}/rules/{effectiveAt}", "rule-based-dimension-rules-list"},
	}
	for _, c := range cases {
		if got := resourceFromPath(c.in); got != c.want {
			t.Errorf("resourceFromPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestActionFromOperation(t *testing.T) {
	cases := []struct {
		method, path, want string
	}{
		{"get", "/x/v1/orgs/{orgId}/budgets", "list"},
		{"get", "/x/v1/orgs/{orgId}/budgets/{id}", "get"},
		{"post", "/x/v1/orgs/{orgId}/budgets", "create"},
		{"post", "/x/v1/orgs/{orgId}/budgets/{id}", "action"},
		{"post", "/x/v1/orgs/{orgId}/budgets/{id}/evaluate", "action"},
		{"put", "/x/v1/orgs/{orgId}/budgets/{id}", "replace"},
		{"patch", "/x/v1/orgs/{orgId}/budgets/{id}", "update"},
		{"delete", "/x/v1/orgs/{orgId}/budgets/{id}", "delete"},
	}
	for _, c := range cases {
		if got := actionFromOperation(c.method, c.path); got != c.want {
			t.Errorf("action(%s %s) = %q, want %q", c.method, c.path, got, c.want)
		}
	}
}

func TestAnnotateForCLI(t *testing.T) {
	doc := map[string]interface{}{
		"paths": map[string]interface{}{
			"/budget/v1/orgs/{orgId}/budgets": map[string]interface{}{
				"get": map[string]interface{}{
					"operationId": "BudgetIndex",
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/BudgetCollection",
									},
								},
							},
						},
					},
				},
			},
		},
		"components": map[string]interface{}{
			"schemas": map[string]interface{}{
				"BudgetCollection": map[string]interface{}{
					"properties": map[string]interface{}{
						"nextPage": map[string]interface{}{"type": "string"},
						"values": map[string]interface{}{
							"type":  "array",
							"items": map[string]interface{}{"$ref": "#/components/schemas/Budget"},
						},
					},
				},
			},
		},
	}
	annotateForCLI(doc)

	op := doc["paths"].(map[string]interface{})["/budget/v1/orgs/{orgId}/budgets"].(map[string]interface{})["get"].(map[string]interface{})

	if got := op["x-flexera-resource"]; got != "budget" {
		t.Errorf("x-flexera-resource = %v, want budget", got)
	}
	if got := op["x-flexera-action"]; got != "list" {
		t.Errorf("x-flexera-action = %v, want list", got)
	}
	if got := op["x-flexera-paginated"]; got != true {
		t.Errorf("x-flexera-paginated = %v, want true", got)
	}
	if got := op["x-flexera-list-item-ref"]; got != "#/components/schemas/Budget" {
		t.Errorf("x-flexera-list-item-ref = %v", got)
	}

	// Idempotency.
	annotateForCLI(doc)
	if got := op["x-flexera-action"]; got != "list" {
		t.Errorf("non-idempotent: action = %v", got)
	}
}
