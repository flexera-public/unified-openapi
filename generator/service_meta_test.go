package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestServiceTitle(t *testing.T) {
	cases := []struct {
		spec SpecConfig
		want string
	}{
		{SpecConfig{Name: "RightScale Bill Analysis API"}, "Bill Analysis"},
		{SpecConfig{Name: "Flexera Identity and Access Management API"}, "Identity and Access Management"},
		{SpecConfig{Name: "Flexera FinOps Billing API"}, "FinOps Billing"},
		{SpecConfig{Name: "RightScale Optima Bill Upload API", Title: "Bill Upload"}, "Bill Upload"},
		{SpecConfig{Service: "x"}, "x"},
	}
	for _, tc := range cases {
		if got := serviceTitle(tc.spec); got != tc.want {
			t.Errorf("serviceTitle(%q) = %q, want %q", tc.spec.Name, got, tc.want)
		}
	}
}

func op(tags ...string) map[string]interface{} {
	out := make([]interface{}, len(tags))
	for i, tag := range tags {
		out[i] = tag
	}
	return map[string]interface{}{"tags": out}
}

func TestApplyServiceMetadata(t *testing.T) {
	iamDoc := map[string]interface{}{"paths": map[string]interface{}{
		"/iam/v1/orgs/{orgId}/projects": map[string]interface{}{"get": op("Project")},
		"/iam/v1/orgs/{orgId}/groups":   map[string]interface{}{"get": op("Group"), "post": op("Group")},
	}}
	grsDoc := map[string]interface{}{"paths": map[string]interface{}{
		"/grs/users/{userId}/projects": map[string]interface{}{"get": op("Project")},
	}}
	stampOperationService(iamDoc, "iam")
	stampOperationService(grsDoc, "grs")

	paths := map[string]interface{}{}
	for _, d := range []map[string]interface{}{iamDoc, grsDoc} {
		for k, v := range d["paths"].(map[string]interface{}) {
			paths[k] = v
		}
	}
	doc := map[string]interface{}{
		"paths": paths,
		"tags": []interface{}{
			map[string]interface{}{"name": "Group"},
			map[string]interface{}{"name": "Project"},
		},
	}
	services := []serviceInfo{
		serviceInfoFromSpec(SpecConfig{ID: "flexera-iam-v1", Name: "Flexera Identity and Access Management API", Vendor: "flexera", Service: "iam", Version: "v1"}),
		serviceInfoFromSpec(SpecConfig{ID: "flexera-grs", Name: "Flexera Global Resource Service API", Vendor: "flexera", Service: "grs", Version: "v2"}),
		// Registered but contributes no operations: omitted from output.
		serviceInfoFromSpec(SpecConfig{ID: "empty", Name: "Empty API", Service: "empty"}),
	}
	if err := applyServiceMetadata(doc, services); err != nil {
		t.Fatalf("applyServiceMetadata() error = %v", err)
	}

	if got := paths["/iam/v1/orgs/{orgId}/groups"].(map[string]interface{})["post"].(map[string]interface{})[extService]; got != "iam" {
		t.Errorf("operation service = %v, want iam", got)
	}

	tags := doc["tags"].([]interface{})
	if got := tags[1].(map[string]interface{})[extServices]; !reflect.DeepEqual(got, []interface{}{"grs", "iam"}) {
		t.Errorf("shared tag services = %v, want [grs iam]", got)
	}
	if got := tags[0].(map[string]interface{})[extServices]; !reflect.DeepEqual(got, []interface{}{"iam"}) {
		t.Errorf("tag services = %v, want [iam]", got)
	}

	registry := doc[extServices].(map[string]interface{})
	if _, ok := registry["empty"]; ok {
		t.Errorf("service without operations should be omitted")
	}
	iam := registry["iam"].(map[string]interface{})
	want := map[string]interface{}{
		"title": "Identity and Access Management", "name": "Flexera Identity and Access Management API",
		"vendor": "flexera", "version": "v1", "specId": "flexera-iam-v1", "operationIdPrefix": "Iam",
		"tags": []interface{}{"Group", "Project"},
	}
	if !reflect.DeepEqual(iam, want) {
		t.Errorf("iam registry entry = %#v, want %#v", iam, want)
	}

	groups := doc[extTagGroups].([]interface{})
	if len(groups) != 2 || groups[0].(map[string]interface{})["name"] != "Global Resource Service" {
		t.Errorf("x-tagGroups = %#v, want sorted by title starting with Global Resource Service", groups)
	}
}

func TestApplyServiceMetadataRequiresEveryOperation(t *testing.T) {
	doc := map[string]interface{}{"paths": map[string]interface{}{
		"/x": map[string]interface{}{"get": op("X")},
	}}
	err := applyServiceMetadata(doc, []serviceInfo{{ID: "x", SpecID: "x"}})
	if err == nil || !strings.Contains(err.Error(), "has no x-flexera-service") {
		t.Fatalf("error = %v, want missing service error", err)
	}

	stampOperationService(doc, "y")
	err = applyServiceMetadata(doc, []serviceInfo{{ID: "x", SpecID: "x"}})
	if err == nil || !strings.Contains(err.Error(), "unknown service") {
		t.Fatalf("error = %v, want unknown service error", err)
	}
}

func TestApplyServiceMetadataRejectsDuplicateServiceIDs(t *testing.T) {
	doc := map[string]interface{}{"paths": map[string]interface{}{}}
	err := applyServiceMetadata(doc, []serviceInfo{{ID: "x", SpecID: "a"}, {ID: "x", SpecID: "b"}})
	if err == nil || !strings.Contains(err.Error(), "used by specs") {
		t.Fatalf("error = %v, want duplicate id error", err)
	}
}
