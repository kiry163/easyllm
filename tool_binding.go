package easyllm

import (
	"fmt"
	"reflect"
)

func BindArgs[T any](raw map[string]any) (T, error) {
	out, _, err := bindArgs[T](raw, UnknownArgumentsReject)
	return out, err
}

func bindArgs[T any](raw map[string]any, unknownPolicy UnknownArgumentPolicy) (T, []ToolArgumentIssue, error) {
	var out T
	target := reflect.ValueOf(&out).Elem()
	if target.Kind() != reflect.Struct {
		return out, nil, fmt.Errorf("tool args target must be a struct")
	}
	if _, err := schemaForType(target.Type(), true); err != nil {
		return out, nil, err
	}
	if raw == nil {
		raw = map[string]any{}
	}
	var issues []ToolArgumentIssue
	context := &bindingContext{recursionDepth: map[recursionField]int{}}
	if err := bindStructWithContext(target, raw, "", unknownPolicy, &issues, context); err != nil {
		return out, issues, err
	}
	return out, issues, nil
}

type bindingContext struct {
	path           []reflect.Type
	recursionDepth map[recursionField]int
}

func bindStructWithContext(target reflect.Value, raw map[string]any, path string, unknownPolicy UnknownArgumentPolicy, issues *[]ToolArgumentIssue, context *bindingContext) error {
	typ := target.Type()
	context.path = append(context.path, derefType(typ))
	defer func() {
		context.path = context.path[:len(context.path)-1]
	}()
	allowed := map[string]struct{}{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Anonymous {
			return fmt.Errorf("anonymous tool field %s is unsupported; use a named field", field.Name)
		}
		if field.PkgPath != "" {
			continue
		}
		tag, err := parseToolTag(field)
		if err != nil {
			return err
		}
		if tag.legacyName {
			return fmt.Errorf("tool field %s uses unsupported name=; use a json tag instead", field.Name)
		}
		name, err := fieldName(field)
		if err != nil {
			return err
		}
		if name == "-" {
			continue
		}
		if _, exists := allowed[name]; exists {
			return fmt.Errorf("duplicate tool JSON field %q", name)
		}
		allowed[name] = struct{}{}
		value, ok := raw[name]
		fieldPath := name
		if path != "" {
			fieldPath = path + "." + name
		}
		recursive := typeInPath(recursiveBaseType(field.Type), context.path)
		depthKey := recursionField{owner: typ, index: i}
		depth := 0
		if recursive {
			if tag.MaxDepth == nil {
				return fmt.Errorf("recursive tool field %s requires maxDepth", field.Name)
			}
			depth = context.recursionDepth[depthKey]
			if depth >= *tag.MaxDepth {
				if ok {
					return fmt.Errorf("%s exceeds maxDepth=%d", fieldPath, *tag.MaxDepth)
				}
				continue
			}
		}
		if !ok {
			if tag.Required {
				return fmt.Errorf("%s is required", fieldPath)
			}
			continue
		}
		if recursive {
			context.recursionDepth[depthKey] = depth + 1
		}
		if err := assignValue(target.Field(i), value, fieldPath, unknownPolicy, issues, context); err != nil {
			if recursive {
				restoreRecursionDepth(context.recursionDepth, depthKey, depth)
			}
			return err
		}
		if recursive {
			restoreRecursionDepth(context.recursionDepth, depthKey, depth)
		}
		if err := validateConstraints(target.Field(i), tag, fieldPath); err != nil {
			return err
		}
	}
	for name := range raw {
		if _, ok := allowed[name]; !ok {
			fieldPath := name
			if path != "" {
				fieldPath = path + "." + name
			}
			if err := handleUnknownArgument(fieldPath, unknownPolicy, issues); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateTypedArgs[T any](args T, raw map[string]any, unknownPolicy UnknownArgumentPolicy) ([]ToolArgumentIssue, error) {
	target := reflect.ValueOf(args)
	if target.Kind() != reflect.Struct {
		return nil, fmt.Errorf("tool args target must be a struct")
	}
	if raw == nil {
		raw = map[string]any{}
	}
	var issues []ToolArgumentIssue
	context := &bindingContext{recursionDepth: map[recursionField]int{}}
	if err := validateTypedStructWithContext(target, raw, "", unknownPolicy, &issues, context); err != nil {
		return issues, err
	}
	return issues, nil
}

func validateTypedStructWithContext(target reflect.Value, raw map[string]any, path string, unknownPolicy UnknownArgumentPolicy, issues *[]ToolArgumentIssue, context *bindingContext) error {
	target = derefValue(target)
	if !target.IsValid() {
		return nil
	}
	typ := target.Type()
	context.path = append(context.path, derefType(typ))
	defer func() {
		context.path = context.path[:len(context.path)-1]
	}()
	allowed := map[string]struct{}{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Anonymous {
			return fmt.Errorf("anonymous tool field %s is unsupported; use a named field", field.Name)
		}
		if field.PkgPath != "" {
			continue
		}
		tag, err := parseToolTag(field)
		if err != nil {
			return err
		}
		if tag.legacyName {
			return fmt.Errorf("tool field %s uses unsupported name=; use a json tag instead", field.Name)
		}
		name, err := fieldName(field)
		if err != nil {
			return err
		}
		if name == "-" {
			continue
		}
		if _, exists := allowed[name]; exists {
			return fmt.Errorf("duplicate tool JSON field %q", name)
		}
		allowed[name] = struct{}{}
		value, ok := raw[name]
		fieldPath := joinArgumentPath(path, name)
		recursive := typeInPath(recursiveBaseType(field.Type), context.path)
		depthKey := recursionField{owner: typ, index: i}
		depth := 0
		if recursive {
			if tag.MaxDepth == nil {
				return fmt.Errorf("recursive tool field %s requires maxDepth", field.Name)
			}
			depth = context.recursionDepth[depthKey]
			if depth >= *tag.MaxDepth {
				if ok {
					return fmt.Errorf("%s exceeds maxDepth=%d", fieldPath, *tag.MaxDepth)
				}
				continue
			}
		}
		if !ok {
			if tag.Required {
				return fmt.Errorf("%s is required", fieldPath)
			}
			continue
		}
		fieldValue := target.Field(i)
		if err := validateConstraints(fieldValue, tag, fieldPath); err != nil {
			return err
		}
		if recursive {
			context.recursionDepth[depthKey] = depth + 1
		}
		if err := validateTypedNested(fieldValue, value, fieldPath, unknownPolicy, issues, context); err != nil {
			if recursive {
				restoreRecursionDepth(context.recursionDepth, depthKey, depth)
			}
			return err
		}
		if recursive {
			restoreRecursionDepth(context.recursionDepth, depthKey, depth)
		}
	}
	for name := range raw {
		if _, ok := allowed[name]; !ok {
			if err := handleUnknownArgument(joinArgumentPath(path, name), unknownPolicy, issues); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateTypedNested(target reflect.Value, raw any, path string, unknownPolicy UnknownArgumentPolicy, issues *[]ToolArgumentIssue, context *bindingContext) error {
	target = derefValue(target)
	if !target.IsValid() {
		return nil
	}
	switch target.Kind() {
	case reflect.Struct:
		obj, ok := raw.(map[string]any)
		if !ok {
			return nil
		}
		return validateTypedStructWithContext(target, obj, path, unknownPolicy, issues, context)
	case reflect.Slice, reflect.Array:
		items, ok := raw.([]any)
		if !ok {
			return nil
		}
		limit := target.Len()
		if len(items) < limit {
			limit = len(items)
		}
		for i := 0; i < limit; i++ {
			if err := validateTypedNested(target.Index(i), items[i], fmt.Sprintf("%s[%d]", path, i), unknownPolicy, issues, context); err != nil {
				return err
			}
		}
	}
	return nil
}

func handleUnknownArgument(path string, policy UnknownArgumentPolicy, issues *[]ToolArgumentIssue) error {
	switch policy {
	case UnknownArgumentsWarn:
		*issues = append(*issues, ToolArgumentIssue{
			Code:    "unknown_argument",
			Path:    path,
			Message: fmt.Sprintf("%s is not defined by the tool schema", path),
		})
		return nil
	case UnknownArgumentsIgnore:
		return nil
	default:
		return fmt.Errorf("%s is not allowed", path)
	}
}

func joinArgumentPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "." + name
}

func validateConstraints(value reflect.Value, tag toolTag, path string) error {
	value = derefValue(value)
	if tag.MinLength != nil || tag.MaxLength != nil {
		if value.Kind() == reflect.String {
			length := len([]rune(value.String()))
			if tag.MinLength != nil && length < *tag.MinLength {
				return fmt.Errorf("%s length must be >= %d", path, *tag.MinLength)
			}
			if tag.MaxLength != nil && length > *tag.MaxLength {
				return fmt.Errorf("%s length must be <= %d", path, *tag.MaxLength)
			}
		}
	}
	if tag.MinItems != nil || tag.MaxItems != nil {
		if value.Kind() == reflect.Slice || value.Kind() == reflect.Array {
			length := value.Len()
			if tag.MinItems != nil && length < *tag.MinItems {
				return fmt.Errorf("%s item count must be >= %d", path, *tag.MinItems)
			}
			if tag.MaxItems != nil && length > *tag.MaxItems {
				return fmt.Errorf("%s item count must be <= %d", path, *tag.MaxItems)
			}
		}
	}
	if len(tag.Enum) > 0 {
		current := fmt.Sprint(value.Interface())
		matched := false
		for _, allowed := range tag.Enum {
			if current == fmt.Sprint(allowed) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s must be one of %v", path, tag.Enum)
		}
	}
	if tag.Minimum != nil || tag.Maximum != nil {
		current, ok := numericReflectValue(value)
		if !ok {
			return nil
		}
		if tag.Minimum != nil && current < *tag.Minimum {
			return fmt.Errorf("%s must be >= %s", path, formatNumber(*tag.Minimum))
		}
		if tag.Maximum != nil && current > *tag.Maximum {
			return fmt.Errorf("%s must be <= %s", path, formatNumber(*tag.Maximum))
		}
	}
	return nil
}

func derefValue(value reflect.Value) reflect.Value {
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return value
		}
		value = value.Elem()
	}
	return value
}

func numericReflectValue(value reflect.Value) (float64, bool) {
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(value.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(value.Uint()), true
	case reflect.Float32, reflect.Float64:
		return value.Float(), true
	default:
		return 0, false
	}
}

func formatNumber(value float64) string {
	if value == float64(int64(value)) {
		return fmt.Sprintf("%d", int64(value))
	}
	return fmt.Sprintf("%g", value)
}

func assignValue(target reflect.Value, raw any, path string, unknownPolicy UnknownArgumentPolicy, issues *[]ToolArgumentIssue, context *bindingContext) error {
	if !target.CanSet() {
		return fmt.Errorf("%s is not assignable", path)
	}
	targetType := target.Type()
	if targetType.Kind() == reflect.Pointer {
		elem := reflect.New(targetType.Elem())
		if err := assignValue(elem.Elem(), raw, path, unknownPolicy, issues, context); err != nil {
			return err
		}
		target.Set(elem)
		return nil
	}
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
		return bindStructWithContext(target, obj, path, unknownPolicy, issues, context)
	case reflect.Slice:
		items, ok := raw.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		slice := reflect.MakeSlice(targetType, 0, len(items))
		for i, item := range items {
			elem := reflect.New(targetType.Elem()).Elem()
			if err := assignValue(elem, item, fmt.Sprintf("%s[%d]", path, i), unknownPolicy, issues, context); err != nil {
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

func restoreRecursionDepth(depths map[recursionField]int, key recursionField, previous int) {
	if previous == 0 {
		delete(depths, key)
		return
	}
	depths[key] = previous
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
