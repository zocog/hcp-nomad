package strings

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	sdk "github.com/hashicorp/sentinel-sdk"
)

var nullTyp = reflect.ValueOf(sdk.Null).Type()
var undefinedTyp = reflect.ValueOf(sdk.Undefined).Type()

func join(a interface{}, sep string) (string, error) {
	return joinReflect(reflect.ValueOf(a), sep)
}

func joinReflect(a reflect.Value, sep string) (string, error) {
	if a.Kind() != reflect.Slice {
		return "", joinErr(a)
	}

	if a.Len() < 1 {
		return "", nil
	}

	var b strings.Builder
	var i int
	for {
		v := a.Index(i)

		if v.Kind() == reflect.Interface {
			// Multi-dimensional lists will contain interface types, so we
			// need to get inner values.
			v = v.Elem()
		}

		// Print out all primitives. The handlers below are consistent
		// with our concrete types, the string() builtin conversion
		// function, and what JSON will possibly return for a string,
		// number, or boolean.
		switch v.Kind() {
		case reflect.String:
			b.WriteString(v.Interface().(string))

		case reflect.Int64:
			b.WriteString(strconv.FormatInt(v.Interface().(int64), 10))

		case reflect.Float64:
			b.WriteString(strconv.FormatFloat(v.Interface().(float64), 'f', -1, 64))

		case reflect.Bool:
			b.WriteString(strconv.FormatBool(v.Interface().(bool)))

		case reflect.Slice:
			s, err := joinReflect(v, sep)
			if err != nil {
				return "", err
			}

			b.WriteString(s)

		default:
			return "", joinErr(v)
		}

		if i >= a.Len()-1 {
			break
		}

		b.WriteString(sep)
		i++
	}

	return b.String(), nil
}

func joinErr(v reflect.Value) error {
	var t string
	switch v.Type() {
	case nullTyp:
		t = "null"
	case undefinedTyp:
		t = "undefined"
	default:
		t = v.Kind().String()
	}

	return fmt.Errorf("invalid type for joining: %s", t)
}
