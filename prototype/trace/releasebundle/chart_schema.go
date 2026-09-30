package releasebundle

import (
	json "encoding/json/v2"
	"reflect"
)

// Helm performs full schema validation during the separate render/lint stage.
// Here the shape must cover the exact defaults and cannot fetch schema references.
func validateChartSchema(data []byte, defaults map[string]any) error {
	var schema map[string]any
	if len(data) == 0 || len(data) > 65536 || json.Unmarshal(data, &schema) != nil {
		return ErrArchive
	}
	if schema["$schema"] != "https://json-schema.org/draft-07/schema#" || schema["type"] != "object" || schema["additionalProperties"] != false {
		return ErrArchive
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok || len(properties) != len(defaults) || !sameRequired(schema["required"], defaults) {
		return ErrArchive
	}
	for name := range defaults {
		if _, found := properties[name]; !found {
			return ErrArchive
		}
	}
	image, ok := properties["image"].(map[string]any)
	if !ok || image["type"] != "object" || image["additionalProperties"] != false {
		return ErrArchive
	}
	imageProperties, ok := image["properties"].(map[string]any)
	if !ok || len(imageProperties) != 3 || !sameRequired(image["required"], defaults["image"].(map[string]any)) {
		return ErrArchive
	}
	for name := range defaults["image"].(map[string]any) {
		if _, found := imageProperties[name]; !found {
			return ErrArchive
		}
	}
	type node struct {
		value any
		depth int
	}
	pending := []node{{schema, 0}}
	visited := 0
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		visited++
		if visited > 10000 || current.depth > 32 {
			return ErrArchive
		}
		switch value := current.value.(type) {
		case map[string]any:
			if _, found := value["$ref"]; found {
				return ErrArchive
			}
			for _, item := range value {
				pending = append(pending, node{item, current.depth + 1})
			}
		case []any:
			for _, item := range value {
				pending = append(pending, node{item, current.depth + 1})
			}
		}
	}
	return nil
}
func sameRequired(value any, defaults map[string]any) bool {
	items, ok := value.([]any)
	if !ok || len(items) != len(defaults) {
		return false
	}
	seen := make(map[string]bool)
	for _, item := range items {
		name, ok := item.(string)
		if !ok || seen[name] {
			return false
		}
		seen[name] = true
	}
	expected := make(map[string]bool)
	for name := range defaults {
		expected[name] = true
	}
	return reflect.DeepEqual(seen, expected)
}
