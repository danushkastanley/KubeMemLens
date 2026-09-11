package incident

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"
)

type nodeWireInspector struct {
	tokens int
	fields map[reflect.Type]map[string]reflect.Type
}

// Bound arrays and reject duplicate/case-aliased keys before typed allocation.
func decodeNode(data []byte) (NodeBundle, error) {
	if len(data) > MaxNodeBytes {
		return NodeBundle{}, fmt.Errorf("Node incident exceeds %d byte limit", MaxNodeBytes)
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	i := nodeWireInspector{fields: map[reflect.Type]map[string]reflect.Type{}}
	if err := i.value(d, reflect.TypeFor[NodeBundle](), "", 0); err != nil {
		return NodeBundle{}, err
	}
	if _, err := d.Token(); err != io.EOF {
		return NodeBundle{}, fmt.Errorf("unexpected trailing Node incident data")
	}
	var b NodeBundle
	// Embedded observations have a compact wire-size ceiling. File indentation
	// must not make a valid normalised observation fail that independent limit.
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return NodeBundle{}, err
	}
	if err := decodeStrict(compact.Bytes(), &b); err != nil {
		return NodeBundle{}, err
	}
	return b, ValidateNode(b)
}

func (i *nodeWireInspector) value(d *json.Decoder, t reflect.Type, field string, depth int) error {
	i.tokens++
	if depth > 20 || i.tokens > 750000 {
		return fmt.Errorf("Node incident nesting or token limit exceeded")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	if token == nil {
		if t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
			return nil
		}
		return fmt.Errorf("Node incident required value is null")
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == reflect.TypeFor[time.Time]() {
		if value, ok := token.(string); !ok || len(value) > 64 {
			return fmt.Errorf("invalid Node incident timestamp")
		}
		return nil
	}
	switch t.Kind() {
	case reflect.Struct:
		if token != json.Delim('{') {
			return fmt.Errorf("invalid Node incident object")
		}
		fields := i.objectFields(t)
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			child, exists := fields[name]
			if !ok || !exists || seen[name] {
				return fmt.Errorf("unknown or duplicate Node incident field")
			}
			seen[name] = true
			if err := i.value(d, child, name, depth+1); err != nil {
				return err
			}
		}
		if t == reflect.TypeFor[NodeBundle]() {
			for _, name := range []string{"schemaVersion", "capturedAt", "toolVersion", "redacted", "evidence"} {
				if !seen[name] {
					return fmt.Errorf("required Node incident field is missing")
				}
			}
		}
		_, err = d.Token()
		return err
	case reflect.Slice:
		if token != json.Delim('[') {
			return fmt.Errorf("invalid Node incident array")
		}
		limit := nodeArrayLimit(field)
		for count := 0; d.More(); count++ {
			if count >= limit {
				return fmt.Errorf("Node incident %s array limit exceeded", field)
			}
			if err := i.value(d, t.Elem(), "", depth+1); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	case reflect.String:
		value, ok := token.(string)
		if !ok || len(value) > 4096 {
			return fmt.Errorf("invalid Node incident text")
		}
	case reflect.Bool:
		if _, ok := token.(bool); !ok {
			return fmt.Errorf("invalid Node incident boolean")
		}
	default:
		if _, ok := token.(json.Number); !ok {
			return fmt.Errorf("invalid Node incident number")
		}
	}
	return nil
}

func (i *nodeWireInspector) objectFields(t reflect.Type) map[string]reflect.Type {
	if fields, ok := i.fields[t]; ok {
		return fields
	}
	fields := map[string]reflect.Type{}
	for n := 0; n < t.NumField(); n++ {
		f := t.Field(n)
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		if tag == "-" || !f.IsExported() {
			continue
		}
		if f.Anonymous && tag == "" {
			for name, child := range i.objectFields(f.Type) {
				fields[name] = child
			}
			continue
		}
		if tag == "" {
			tag = f.Name
		}
		fields[tag] = f.Type
	}
	i.fields[t] = fields
	return fields
}

func nodeArrayLimit(field string) int {
	switch field {
	case "series":
		return MaxNodeInstances
	case "points":
		return 61
	case "pods", "workloads":
		return 100
	case "signals":
		return 16
	case "caveats":
		return 32
	case "systemContainers":
		return 4
	case "hugepages":
		return 16
	default:
		return 0
	}
}
