package main

import (
	"fmt"
	"sort"
	"strings"
)

// Service metadata extensions emitted into the unified spec. They let
// downstream consumers (SDKs, CLIs, documentation renderers) group operations
// by the API service they came from without inferring it from operationId
// prefixes or path shapes. Operation tags are NOT service-unique after merge
// (e.g. "Project" is contributed by both GRS and IAM), so the service is
// recorded per operation.
//
//	root:      x-flexera-services  map[serviceID]{title,name,vendor,version,specId,operationIdPrefix,sourceUrl,tags}
//	root:      x-tagGroups         Redoc-style [{name: <title>, tags: [...]}]
//	operation: x-flexera-service   serviceID (key into x-flexera-services)
//	tag:       x-flexera-services  sorted []serviceID contributing operations with that tag
const (
	extService   = "x-flexera-service"
	extServices  = "x-flexera-services"
	extTagGroups = "x-tagGroups"

	authServiceID = "auth"
)

// serviceInfo is the registry entry for one merged service.
type serviceInfo struct {
	ID                string
	Title             string
	Name              string
	Vendor            string
	Version           string
	SpecID            string
	OperationIDPrefix string
	SourceURL         string
}

func serviceInfoFromSpec(spec SpecConfig) serviceInfo {
	return serviceInfo{
		ID:                spec.Service,
		Title:             serviceTitle(spec),
		Name:              spec.Name,
		Vendor:            spec.Vendor,
		Version:           spec.Version,
		SpecID:            spec.ID,
		OperationIDPrefix: componentNamespace(spec.Service),
		SourceURL:         spec.Source.URL,
	}
}

func authServiceInfo() serviceInfo {
	return serviceInfo{
		ID:                authServiceID,
		Title:             "Authentication",
		Name:              "Flexera Login Token Service",
		Vendor:            "flexera",
		OperationIDPrefix: componentNamespace(authServiceID),
	}
}

// serviceTitle returns the explicit title or derives one from the spec name by
// dropping the vendor prefix and the trailing " API".
func serviceTitle(spec SpecConfig) string {
	if title := strings.TrimSpace(spec.Title); title != "" {
		return title
	}
	title := strings.TrimSpace(spec.Name)
	for _, prefix := range []string{"Flexera ", "RightScale "} {
		title = strings.TrimPrefix(title, prefix)
	}
	title = strings.TrimSuffix(title, " API")
	if title == "" {
		return spec.Service
	}
	return title
}

// stampOperationService tags every operation in a single-service document.
func stampOperationService(doc map[string]interface{}, serviceID string) {
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
			if operation, ok := pathItem[method].(map[string]interface{}); ok {
				operation[extService] = serviceID
			}
		}
	}
}

// applyServiceMetadata writes the root registry, per-tag service lists, and
// x-tagGroups. It fails if any operation lacks x-flexera-service or references
// an unregistered service, so the metadata stays complete.
func applyServiceMetadata(doc map[string]interface{}, services []serviceInfo) error {
	registry := make(map[string]serviceInfo, len(services))
	for _, svc := range services {
		if svc.ID == "" {
			return fmt.Errorf("service metadata: spec %q has no service id", svc.SpecID)
		}
		if previous, exists := registry[svc.ID]; exists {
			return fmt.Errorf("service metadata: service id %q used by specs %q and %q", svc.ID, previous.SpecID, svc.SpecID)
		}
		registry[svc.ID] = svc
	}

	paths, ok := doc["paths"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("service metadata: unified spec has no paths")
	}
	tagServices := map[string]map[string]bool{}
	serviceTags := map[string]map[string]bool{}
	for pathKey, pathItemValue := range paths {
		pathItem, ok := pathItemValue.(map[string]interface{})
		if !ok {
			continue
		}
		for _, method := range operationMethods {
			operation, ok := pathItem[method].(map[string]interface{})
			if !ok {
				continue
			}
			serviceID, _ := operation[extService].(string)
			if serviceID == "" {
				return fmt.Errorf("service metadata: %s %s has no %s", strings.ToUpper(method), pathKey, extService)
			}
			if _, known := registry[serviceID]; !known {
				return fmt.Errorf("service metadata: %s %s references unknown service %q", strings.ToUpper(method), pathKey, serviceID)
			}
			tags, _ := operation["tags"].([]interface{})
			for _, tagValue := range tags {
				tag, _ := tagValue.(string)
				if tag == "" {
					continue
				}
				if tagServices[tag] == nil {
					tagServices[tag] = map[string]bool{}
				}
				tagServices[tag][serviceID] = true
				if serviceTags[serviceID] == nil {
					serviceTags[serviceID] = map[string]bool{}
				}
				serviceTags[serviceID][tag] = true
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
			if ids := sortedKeys(tagServices[name]); len(ids) > 0 {
				tag[extServices] = toInterfaceSlice(ids)
			}
		}
	}

	ids := make([]string, 0, len(registry))
	for id := range registry {
		if len(serviceTags[id]) > 0 {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		return registry[ids[i]].Title < registry[ids[j]].Title
	})

	rootServices := make(map[string]interface{}, len(ids))
	groups := make([]interface{}, 0, len(ids))
	for _, id := range ids {
		svc := registry[id]
		tags := toInterfaceSlice(sortedKeys(serviceTags[id]))
		entry := map[string]interface{}{
			"title":             svc.Title,
			"name":              svc.Name,
			"vendor":            svc.Vendor,
			"operationIdPrefix": svc.OperationIDPrefix,
			"tags":              tags,
		}
		if svc.Version != "" {
			entry["version"] = svc.Version
		}
		if svc.SpecID != "" {
			entry["specId"] = svc.SpecID
		}
		if svc.SourceURL != "" {
			entry["sourceUrl"] = svc.SourceURL
		}
		rootServices[id] = entry
		groups = append(groups, map[string]interface{}{"name": svc.Title, "tags": tags})
	}
	doc[extServices] = rootServices
	doc[extTagGroups] = groups
	return nil
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func toInterfaceSlice(values []string) []interface{} {
	out := make([]interface{}, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}
