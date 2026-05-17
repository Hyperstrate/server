package validation

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

// BindingErrors converts a ShouldBindJSON error into a human-readable summary
// and a map of JSON field names to error message slices. Returns nil fields
// when the error is not a validator.ValidationErrors (e.g. malformed JSON).
func BindingErrors(err error, input any) (summary string, fields map[string][]string) {
	var ve validator.ValidationErrors
	if !errors.As(err, &ve) {
		return err.Error(), nil
	}

	jsonKeys := structJSONKeys(reflect.TypeOf(input))

	fields = make(map[string][]string)
	for _, fe := range ve {
		key := fe.Field()
		if jk, ok := jsonKeys[fe.Field()]; ok {
			key = jk
		}
		fields[key] = append(fields[key], fieldMessage(fe))
	}

	return "validation failed", fields
}

// structJSONKeys returns a map of struct field name → json tag key for the given type.
func structJSONKeys(t reflect.Type) map[string]string {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	m := make(map[string]string, t.NumField())
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name != "" {
			m[f.Name] = name
		}
	}
	return m
}

func fieldMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "this field is required"
	case "url", "http_url":
		return "must be a valid URL"
	case "email":
		return "must be a valid email address"
	case "min":
		if fe.Kind() == reflect.String {
			return fmt.Sprintf("must be at least %s characters", fe.Param())
		}
		return fmt.Sprintf("must be at least %s", fe.Param())
	case "max":
		if fe.Kind() == reflect.String {
			return fmt.Sprintf("must be at most %s characters", fe.Param())
		}
		return fmt.Sprintf("must be at most %s", fe.Param())
	case "gte":
		return fmt.Sprintf("must be at least %s", fe.Param())
	case "lte":
		return fmt.Sprintf("must be at most %s", fe.Param())
	case "oneof":
		return fmt.Sprintf("must be one of: %s", fe.Param())
	case "uuid", "uuid4":
		return "must be a valid UUID"
	default:
		return fmt.Sprintf("failed validation: %s", fe.Tag())
	}
}
