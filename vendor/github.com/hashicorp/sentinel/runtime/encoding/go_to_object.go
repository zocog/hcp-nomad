package encoding

import (
	"errors"
	"fmt"
	"reflect"

	sdk "github.com/hashicorp/sentinel-sdk"
	"github.com/hashicorp/sentinel/lang/object"
	"github.com/hashicorp/sentinel/lang/token"
)

var (
	sdkNullValue      = reflect.ValueOf(sdk.Null)
	sdkUndefinedValue = reflect.ValueOf(sdk.Undefined)
)

// GoToObject converts the Go value to an Object.
//
// The Go value must contain only primitives, collections of primitives,
// and structures. It must not contain any other type of value or an error
// will be returned.
//
// The primitive types byte and rune are aliases to integer types (as
// defined by the Go spec) and are treated as integers in conversion.
func GoToObject(raw interface{}) (object.Object, error) {
	d := &objectDecoder{}
	return d.goToObject(raw)
}

// GoToObjectWithPos converts the Go value to an Object, identical to
// GoToObject, but takes a position to allow for the encoding of
// undefined values.
//
// Use when the data could contain sdk.Undefined (ie: return data
// from a plugin call).
func GoToObjectWithPos(raw interface{}, pos token.Pos) (object.Object, error) {
	d := &objectDecoder{pos: pos}
	return d.goToObject(raw)
}

// objectTyp is a reflect.Type for Object
var objectTyp = reflect.TypeOf((*object.Object)(nil)).Elem()

// objectDecoder is the deep object decoder. It holds state that may
// be required during decoding (namely an optional source position).
//
// If pos is defined, any sdk.Undefined values encountered will be
// properly decoded as undefined with the relevant position info.
// Otherwise, the behavior for sdk.Undefined is undefined in the
// sense that it's unhandled. :P
type objectDecoder struct {
	pos token.Pos
}

func (d *objectDecoder) goToObject(raw interface{}) (object.Object, error) {
	// First try the cheaper and more common cases with primitive types.
	// Using a type switch like this instead of reflect is about twice
	// as fast (half the number of allocations).
	switch v := raw.(type) {
	case object.Object:
		return v, nil

	case nil:
		return object.Null, nil

	case bool:
		return object.Bool(v), nil

	case int8:
		return &object.IntObj{Value: int64(v)}, nil

	case int16:
		return &object.IntObj{Value: int64(v)}, nil

	case int32:
		return &object.IntObj{Value: int64(v)}, nil

	case int64:
		return &object.IntObj{Value: v}, nil

	case uint8:
		return &object.IntObj{Value: int64(v)}, nil

	case uint16:
		return &object.IntObj{Value: int64(v)}, nil

	case uint32:
		return &object.IntObj{Value: int64(v)}, nil

	case uint64:
		return &object.IntObj{Value: int64(v)}, nil

	case float32:
		return &object.FloatObj{Value: float64(v)}, nil

	case float64:
		return &object.FloatObj{Value: v}, nil

	case complex64, complex128:
		return nil, errors.New("cannot convert complex number to Sentinel")

	case string:
		return &object.StringObj{Value: v}, nil
	}

	// Null
	if raw == sdk.Null {
		return object.Null, nil
	}

	// Undefined
	if raw == sdk.Undefined && d.pos > 0 {
		return &object.UndefinedObj{Pos: []token.Pos{d.pos}}, nil
	}

	// Otherwise, we have a more complex type and must use reflection.
	return d.toObject_reflect(reflect.ValueOf(raw))
}

func (d *objectDecoder) toObject_reflect(v reflect.Value) (object.Object, error) {
	// Null pointer
	if !v.IsValid() {
		return object.Null, nil
	}

	// If we have a value that is an Object, return that
	if v.Type().Implements(objectTyp) {
		return v.Interface().(object.Object), nil
	}

	// If the value is the special SDK null value, then return null
	if v == sdkNullValue {
		return object.Null, nil
	}

	// If the value is the special SDK undefined value, and we have a
	// non-zero position, return undefined.
	if v == sdkUndefinedValue && d.pos > 0 {
		return &object.UndefinedObj{Pos: []token.Pos{d.pos}}, nil
	}

	// Decode depending on the type. We need to redo all of the primitives
	// above unfortunately since they may fall to this point if they're
	// wrapped in an interface type.
	switch v.Kind() {
	case reflect.Interface:
		return d.toObject_reflect(v.Elem())

	case reflect.Ptr:
		return d.toObject_reflect(v.Elem())

	case reflect.Bool:
		return object.Bool(v.Bool()), nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return &object.IntObj{Value: v.Int()}, nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &object.IntObj{Value: int64(v.Uint())}, nil

	case reflect.Float32, reflect.Float64:
		return &object.FloatObj{Value: v.Float()}, nil

	case reflect.Complex64, reflect.Complex128:
		return nil, errors.New("cannot convert complex number to Sentinel value")

	case reflect.String:
		return &object.StringObj{Value: v.String()}, nil

	case reflect.Array, reflect.Slice:
		return d.toObject_array(v)

	case reflect.Map:
		return d.toObject_map(v)

	case reflect.Struct:
		return d.toObject_struct(v)

	case reflect.Chan:
		return nil, errors.New("cannot convert channel to Sentinel value")

	case reflect.Func:
		return nil, errors.New("cannot convert func to Sentinel value")
	}

	return nil, fmt.Errorf("cannot convert type %s to Sentinel value", v.Kind())
}

func (d *objectDecoder) toObject_array(v reflect.Value) (object.Object, error) {
	result := &object.ListObj{Elts: make([]object.Object, v.Len())}
	for i := range result.Elts {
		elem, err := d.toObject_reflect(v.Index(i))
		if err != nil {
			return nil, err
		}

		result.Elts[i] = elem
	}

	return result, nil
}

func (d *objectDecoder) toObject_map(v reflect.Value) (object.Object, error) {
	result := &object.MapObj{Elts: make([]object.KeyedObj, v.Len())}
	for i, keyV := range v.MapKeys() {
		key, err := d.toObject_reflect(keyV)
		if err != nil {
			return nil, err
		}

		value, err := d.toObject_reflect(v.MapIndex(keyV))
		if err != nil {
			return nil, err
		}

		result.Elts[i] = object.KeyedObj{Key: key, Value: value}
	}

	return result, nil
}

func (d *objectDecoder) toObject_struct(v reflect.Value) (object.Object, error) {
	// Get the type since we need this to determine what is exported,
	// field tags, etc.
	t := v.Type()

	result := &object.MapObj{Elts: make([]object.KeyedObj, 0, t.NumField())}
	for i := 0; i < cap(result.Elts); i++ {
		field := t.Field(i)

		// If PkgPath is non-empty, this is unexported and can be ignored
		if field.PkgPath != "" {
			continue
		}

		// Determine the map key
		key := field.Name
		if v, ok := field.Tag.Lookup("sentinel"); ok {
			// A blank value means to not export this value
			if v == "" {
				continue
			}

			key = v
		}

		// Convert the value
		value, err := d.toObject_reflect(v.Field(i))
		if err != nil {
			return nil, err
		}

		result.Elts = append(result.Elts, object.KeyedObj{
			Key:   &object.StringObj{Value: key},
			Value: value,
		})
	}

	return result, nil
}
