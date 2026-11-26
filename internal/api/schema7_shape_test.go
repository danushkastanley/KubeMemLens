package api

import (
	"reflect"
	"strings"
)

// schema8Fields are the only fields snapshot schema 8 added to the read and
// agent types. Removing them rebuilds a type as schema 7, and main before this
// change, declared it, so a strict decoder of the result accepts exactly what
// a schema 7 reader accepts.
var schema8Fields = map[string]bool{"PodCgroup": true, "Zswap": true, "OOMGroupKill": true}

// schema7Shape returns t without the schema 8 fields. Only struct types that
// contain them are rebuilt.
func schema7Shape(t reflect.Type) reflect.Type {
	switch t.Kind() {
	case reflect.Slice:
		if element := schema7Shape(t.Elem()); element != t.Elem() {
			return reflect.SliceOf(element)
		}
		return t
	case reflect.Struct:
		return schema7Struct(t)
	default:
		return t
	}
}

func schema7Struct(t reflect.Type) reflect.Type {
	fields := make([]reflect.StructField, 0, t.NumField())
	changed := false
	for index := range t.NumField() {
		field := t.Field(index)
		if schema8Fields[field.Name] {
			changed = true
			continue
		}
		if shaped := schema7Shape(field.Type); shaped != field.Type {
			field.Type, changed = shaped, true
		}
		fields = append(fields, field)
	}
	if !changed {
		return t
	}
	return reflect.StructOf(jsonEquivalentFields(fields))
}

// jsonEquivalentFields replaces embedded fields, which reflect.StructOf cannot
// build when they have methods, with what encoding/json does with them: a
// tagged embedded struct is an ordinary named field, and an untagged one
// contributes its own fields.
func jsonEquivalentFields(fields []reflect.StructField) []reflect.StructField {
	result := make([]reflect.StructField, 0, len(fields))
	for _, field := range fields {
		field.Offset, field.Index = 0, nil
		if !field.Anonymous {
			result = append(result, field)
			continue
		}
		field.Anonymous = false
		if name, _, _ := strings.Cut(field.Tag.Get("json"), ","); name != "" {
			result = append(result, field)
			continue
		}
		for index := range field.Type.NumField() {
			inner := field.Type.Field(index)
			inner.Offset, inner.Index = 0, nil
			result = append(result, inner)
		}
	}
	return result
}
