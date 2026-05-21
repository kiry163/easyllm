package tool

import (
	"fmt"
	"reflect"
	"strings"
	"unicode"
)

type toolTag struct {
	Name        string
	Description string
	Required    bool
}

func SchemaFor[T any]() (map[string]any, error) {
	var zero T
	typ := reflect.TypeOf(zero)
	if typ == nil {
		return nil, fmt.Errorf("tool args type is nil")
	}
	return schemaForType(derefType(typ), true)
}

func schemaForType(typ reflect.Type, topLevel bool) (map[string]any, error) {
	typ = derefType(typ)
	switch typ.Kind() {
	case reflect.Struct:
		properties := map[string]any{}
		required := make([]string, 0)
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			tag := parseToolTag(field)
			name := fieldName(field, tag)
			if name == "-" {
				continue
			}
			fieldSchema, err := schemaForType(field.Type, false)
			if err != nil {
				return nil, err
			}
			if tag.Description != "" {
				fieldSchema["description"] = tag.Description
			}
			properties[name] = fieldSchema
			if tag.Required {
				required = append(required, name)
			}
		}
		out := map[string]any{
			"type":       "object",
			"properties": properties,
		}
		if len(required) > 0 {
			out["required"] = required
		}
		return out, nil
	case reflect.Slice, reflect.Array:
		items, err := schemaForType(typ.Elem(), false)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": "array", "items": items}, nil
	case reflect.String:
		return map[string]any{"type": "string"}, nil
	case reflect.Bool:
		return map[string]any{"type": "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}, nil
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}, nil
	default:
		if topLevel {
			return nil, fmt.Errorf("unsupported top-level tool args type %s", typ.String())
		}
		return nil, fmt.Errorf("unsupported tool field type %s", typ.String())
	}
}

func parseToolTag(field reflect.StructField) toolTag {
	raw := strings.TrimSpace(field.Tag.Get("tool"))
	if raw == "" {
		return toolTag{}
	}
	out := toolTag{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		switch {
		case part == "":
			continue
		case part == "required":
			out.Required = true
		case strings.HasPrefix(part, "name="):
			out.Name = strings.TrimSpace(strings.TrimPrefix(part, "name="))
		case strings.HasPrefix(part, "desc="):
			out.Description = strings.TrimSpace(strings.TrimPrefix(part, "desc="))
		}
	}
	return out
}

func fieldName(field reflect.StructField, tag toolTag) string {
	if tag.Name != "" {
		return tag.Name
	}
	jsonTag := strings.TrimSpace(field.Tag.Get("json"))
	if jsonTag != "" {
		name := strings.Split(jsonTag, ",")[0]
		if name != "" {
			return name
		}
	}
	return toSnakeCase(field.Name)
}

func toSnakeCase(value string) string {
	if value == "" {
		return ""
	}
	var b strings.Builder
	runes := []rune(value)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 && (unicode.IsLower(runes[i-1]) || (i+1 < len(runes) && unicode.IsLower(runes[i+1]))) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func derefType(typ reflect.Type) reflect.Type {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ
}
