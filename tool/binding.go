package tool

import (
	"fmt"
	"reflect"
)

func BindArgs[T any](raw map[string]any) (T, error) {
	var out T
	target := reflect.ValueOf(&out).Elem()
	if target.Kind() != reflect.Struct {
		return out, fmt.Errorf("tool args target must be a struct")
	}
	if raw == nil {
		raw = map[string]any{}
	}
	if err := bindStruct(target, raw, ""); err != nil {
		return out, err
	}
	return out, nil
}

func bindStruct(target reflect.Value, raw map[string]any, path string) error {
	typ := target.Type()
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
		value, ok := raw[name]
		fieldPath := name
		if path != "" {
			fieldPath = path + "." + name
		}
		if !ok {
			if tag.Required {
				return fmt.Errorf("%s is required", fieldPath)
			}
			continue
		}
		if err := assignValue(target.Field(i), value, fieldPath); err != nil {
			return err
		}
	}
	return nil
}

func assignValue(target reflect.Value, raw any, path string) error {
	if !target.CanSet() {
		return fmt.Errorf("%s is not assignable", path)
	}
	targetType := target.Type()
	if targetType.Kind() == reflect.Pointer {
		elem := reflect.New(targetType.Elem())
		if err := assignValue(elem.Elem(), raw, path); err != nil {
			return err
		}
		target.Set(elem)
		return nil
	}
	raw = repairStructuredStringValue(targetType, raw)
	switch targetType.Kind() {
	case reflect.String:
		text, ok := raw.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", path)
		}
		target.SetString(text)
		return nil
	case reflect.Bool:
		value, ok := raw.(bool)
		if !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
		target.SetBool(value)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value, ok := numericValue(raw)
		if !ok {
			return fmt.Errorf("%s must be an integer", path)
		}
		target.SetInt(int64(value))
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value, ok := numericValue(raw)
		if !ok || value < 0 {
			return fmt.Errorf("%s must be a positive integer", path)
		}
		target.SetUint(uint64(value))
		return nil
	case reflect.Float32, reflect.Float64:
		value, ok := floatValue(raw)
		if !ok {
			return fmt.Errorf("%s must be a number", path)
		}
		target.SetFloat(value)
		return nil
	case reflect.Struct:
		obj, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		return bindStruct(target, obj, path)
	case reflect.Slice:
		items, ok := raw.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		slice := reflect.MakeSlice(targetType, 0, len(items))
		for i, item := range items {
			elem := reflect.New(targetType.Elem()).Elem()
			if err := assignValue(elem, item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
			slice = reflect.Append(slice, elem)
		}
		target.Set(slice)
		return nil
	default:
		return fmt.Errorf("%s has unsupported type %s", path, targetType.String())
	}
}

func repairStructuredStringValue(targetType reflect.Type, raw any) any {
	text, ok := raw.(string)
	if !ok {
		return raw
	}

	switch targetType.Kind() {
	case reflect.Struct:
		parsed, _, err := DecodeJSONObjectString(text)
		if err == nil {
			return parsed
		}
	case reflect.Slice:
		if derefType(targetType.Elem()).Kind() != reflect.Struct {
			return raw
		}
		parsed, _, err := DecodeJSONArrayString(text)
		if err != nil {
			return raw
		}
		items := make([]any, 0, len(parsed))
		for _, item := range parsed {
			items = append(items, item)
		}
		return items
	}

	return raw
}

func numericValue(raw any) (int64, bool) {
	switch typed := raw.(type) {
	case int:
		return int64(typed), true
	case int8:
		return int64(typed), true
	case int16:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case uint:
		return int64(typed), true
	case uint8:
		return int64(typed), true
	case uint16:
		return int64(typed), true
	case uint32:
		return int64(typed), true
	case uint64:
		return int64(typed), true
	case float64:
		if typed != float64(int64(typed)) {
			return 0, false
		}
		return int64(typed), true
	default:
		return 0, false
	}
}

func floatValue(raw any) (float64, bool) {
	switch typed := raw.(type) {
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	default:
		return 0, false
	}
}
