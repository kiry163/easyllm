package easyllm

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode"
)

type toolTag struct {
	Name        string
	Description string
	Required    bool
	Enum        []any
	Minimum     *float64
	Maximum     *float64
	MinLength   *int
	MaxLength   *int
	MinItems    *int
	MaxItems    *int
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
			if len(tag.Enum) > 0 {
				fieldSchema["enum"] = tag.Enum
			}
			if tag.Minimum != nil {
				fieldSchema["minimum"] = *tag.Minimum
			}
			if tag.Maximum != nil {
				fieldSchema["maximum"] = *tag.Maximum
			}
			if tag.MinLength != nil {
				fieldSchema["minLength"] = *tag.MinLength
			}
			if tag.MaxLength != nil {
				fieldSchema["maxLength"] = *tag.MaxLength
			}
			if tag.MinItems != nil {
				fieldSchema["minItems"] = *tag.MinItems
			}
			if tag.MaxItems != nil {
				fieldSchema["maxItems"] = *tag.MaxItems
			}
			properties[name] = fieldSchema
			if tag.Required {
				required = append(required, name)
			}
		}
		out := map[string]any{
			"type":                 "object",
			"properties":           properties,
			"additionalProperties": false,
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
		case strings.HasPrefix(part, "enum="):
			out.Enum = parseEnumValues(strings.TrimSpace(strings.TrimPrefix(part, "enum=")))
		case strings.HasPrefix(part, "minimum="):
			if value, ok := parseNumberTag(strings.TrimSpace(strings.TrimPrefix(part, "minimum="))); ok {
				out.Minimum = &value
			}
		case strings.HasPrefix(part, "maximum="):
			if value, ok := parseNumberTag(strings.TrimSpace(strings.TrimPrefix(part, "maximum="))); ok {
				out.Maximum = &value
			}
		case strings.HasPrefix(part, "minLength="):
			if value, ok := parseIntTag(strings.TrimSpace(strings.TrimPrefix(part, "minLength="))); ok {
				out.MinLength = &value
			}
		case strings.HasPrefix(part, "maxLength="):
			if value, ok := parseIntTag(strings.TrimSpace(strings.TrimPrefix(part, "maxLength="))); ok {
				out.MaxLength = &value
			}
		case strings.HasPrefix(part, "minItems="):
			if value, ok := parseIntTag(strings.TrimSpace(strings.TrimPrefix(part, "minItems="))); ok {
				out.MinItems = &value
			}
		case strings.HasPrefix(part, "maxItems="):
			if value, ok := parseIntTag(strings.TrimSpace(strings.TrimPrefix(part, "maxItems="))); ok {
				out.MaxItems = &value
			}
		}
	}
	return out
}

func parseEnumValues(raw string) []any {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, "|")
	out := make([]any, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func parseNumberTag(raw string) (float64, bool) {
	if raw == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func parseIntTag(raw string) (int, bool) {
	if raw == "" {
		return 0, false
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return value, true
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
