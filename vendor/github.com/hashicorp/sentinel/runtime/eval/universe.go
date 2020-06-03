package eval

import (
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/hashicorp/sentinel/lang/object"
)

// Universe returns a copy of the universe scope, which contains the
// pre-declared identifiers that are part of the language specification. This
// should be the parent of any scope used for interpretation.
func Universe() *object.Scope {
	return &object.Scope{
		Objects: map[string]object.Object{
			"true":  object.True,
			"false": object.False,
			"null":  object.Null,

			// Builtins
			"append": object.ExternalFunc(builtin_append),
			"delete": object.ExternalFunc(builtin_delete),
			"keys":   object.ExternalFunc(builtin_keys),
			"values": object.ExternalFunc(builtin_values),
			"length": object.ExternalFunc(builtin_length),
			"range":  object.ExternalFunc(builtin_range),
			"int":    object.ExternalFunc(builtin_int),
			"float":  object.ExternalFunc(builtin_float),
			"string": object.ExternalFunc(builtin_string),
			"bool":   object.ExternalFunc(builtin_bool),
		},
	}
}

// UndefinedName is the global constant for the "undefined" identifier.
const UndefinedName = "undefined"

//-------------------------------------------------------------------
// Builtin Functions
//
// These all have minimal unit tests because the lang/spec tests cover
// the various cases of the built-in functions..

// length
func builtin_length(args []object.Object) (interface{}, error) {
	if err := builtinArgCount(args, 1, 1); err != nil {
		return nil, err
	}

	switch x := args[0].(type) {
	case *object.StringObj:
		return len(x.Value), nil

	case *object.ListObj:
		return len(x.Elts), nil

	case *object.MapObj:
		return len(x.Elts), nil

	case *object.MemoizedRemoteObj:
		return len(x.Context.Elts), nil

	case *object.UndefinedObj:
		return x, nil

	default:
		return nil, fmt.Errorf(
			"length can only be called with strings, lists, or maps, got %q",
			args[0].Type())
	}
}

// append
func builtin_append(args []object.Object) (interface{}, error) {
	res := &object.UndefinedObj{}
	if err := builtinArgCount(args, 2, 2); err != nil {
		return res, err
	}

	x, ok := args[0].(*object.ListObj)
	if !ok {
		return res, fmt.Errorf(
			"append first argument can only be called with lists, got %q",
			args[0].Type())
	}

	x.Elts = append(x.Elts, args[1])
	return res, nil
}

// delete
func builtin_delete(args []object.Object) (interface{}, error) {
	if err := builtinArgCount(args, 2, 2); err != nil {
		return nil, err
	}

	var v *object.MapObj
	switch x := args[0].(type) {
	case *object.MapObj:
		v = x

	case *object.MemoizedRemoteObj:
		v = x.Context

	case *object.UndefinedObj:
		return x, nil

	default:
		return nil, fmt.Errorf(
			"delete first argument can only be called with maps, got %q",
			args[0].Type())
	}

	key := args[1]
	for i, elt := range v.Elts {
		if elt.Key.Type() == key.Type() && elt.Key.String() == key.String() {
			lastIdx := len(v.Elts) - 1
			v.Elts[i] = v.Elts[lastIdx]
			v.Elts[lastIdx] = object.KeyedObj{}
			v.Elts = v.Elts[:lastIdx]
			break
		}
	}

	return &object.UndefinedObj{}, nil
}

// keys
func builtin_keys(args []object.Object) (interface{}, error) {
	if err := builtinArgCount(args, 1, 1); err != nil {
		return nil, err
	}

	var x *object.MapObj
	switch y := args[0].(type) {
	case *object.MapObj:
		x = y

	case *object.MemoizedRemoteObj:
		x = y.Context

	case *object.UndefinedObj:
		return y, nil

	default:
		return nil, fmt.Errorf(
			"keys first argument can only be called with maps, got %q",
			args[0].Type())
	}

	result := make([]object.Object, len(x.Elts))
	for i, elt := range x.Elts {
		result[i] = elt.Key
	}

	return &object.ListObj{Elts: result}, nil
}

// values
func builtin_values(args []object.Object) (interface{}, error) {
	if err := builtinArgCount(args, 1, 1); err != nil {
		return nil, err
	}

	var result []object.Object
	switch x := args[0].(type) {
	case *object.MapObj:
		result = make([]object.Object, len(x.Elts))
		for i, elt := range x.Elts {
			result[i] = elt.Value
		}

	case *object.MemoizedRemoteObj:
		result = make([]object.Object, len(x.Context.Elts))
		for i, elt := range x.Context.Elts {
			v := elt.Value
			if m, ok := elt.Value.(*object.MapObj); ok {
				// Ensure that the metadata on the outer MemoizedRemoteObj
				// follows the value. Increment depth to note that we have
				// traversed one level down.
				v = &object.MemoizedRemoteObj{
					Tag:     x.Tag,
					Depth:   x.Depth + 1,
					Context: m,
				}
			}

			result[i] = v
		}

	case *object.UndefinedObj:
		return x, nil

	default:
		return nil, fmt.Errorf(
			"values first argument can only be called with maps, got %q",
			args[0].Type())
	}

	return &object.ListObj{Elts: result}, nil
}

// range
func builtin_range(args []object.Object) (interface{}, error) {
	const (
		start = iota
		end
		step
	)

	if err := builtinArgCount(args, 1, 3); err != nil {
		return nil, err
	}

	a := []int64{0, 0, 1}
	for i := 0; i < len(args); i++ {
		o, err := builtin_int([]object.Object{args[i]})
		if err != nil {
			return nil, err
		}

		switch x := o.(type) {
		case *object.UndefinedObj:
			return o, nil

		case *object.IntObj:
			a[i] = x.Value

		default:
			// Should never happen
			return nil, fmt.Errorf(
				"range: unexpected type %T from conversion of type %q",
				o, args[i].Type())
		}
	}

	// Flip end and start if only one arg (end only)
	if len(args) < 2 {
		a[start], a[end] = a[end], a[start]
	}

	if a[step] == 0 {
		return nil, errors.New("range step cannot be zero")
	}

	// Range length formula is:
	//  * When step > 0 and start < end:
	//    1 + (end - start - 1) / step
	//
	//  * When step < 0 and start > end:
	//    1 + (start - end - 1) / (0 - step)
	//
	// Impossible ranges outside of these constraints are length 0.
	//
	// Source: cpython/Objects/rangeobject.c,
	//  get_len_of_range
	var l int64
	switch {
	case a[step] > 0 && a[start] < a[end]:
		l = 1 + (a[end]-a[start]-1)/a[step]
	case a[step] < 0 && a[start] > a[end]:
		l = 1 + (a[start]-a[end]-1)/(0-a[step])
	default:
		l = 0
	}

	r := make([]object.Object, l)
	for i := int64(0); i < l; i++ {
		r[i] = &object.IntObj{Value: a[start] + a[step]*i}
	}

	return &object.ListObj{Elts: r}, nil
}

// int
func builtin_int(args []object.Object) (interface{}, error) {
	if err := builtinArgCount(args, 1, 1); err != nil {
		return nil, err
	}

	switch x := args[0].(type) {
	case *object.IntObj:
		return x, nil

	case *object.StringObj:
		v, err := strconv.ParseInt(x.Value, 0, 64)
		if err != nil {
			return nil, err
		}

		return &object.IntObj{Value: v}, nil

	case *object.FloatObj:
		return &object.IntObj{
			Value: int64(math.Floor(x.Value)),
		}, nil

	case *object.BoolObj:
		if x.Value {
			return &object.IntObj{Value: 1}, nil
		} else {
			return &object.IntObj{Value: 0}, nil
		}

	case *object.UndefinedObj:
		return x, nil

	default:
		return &object.UndefinedObj{}, nil
	}
}

// float
func builtin_float(args []object.Object) (interface{}, error) {
	if err := builtinArgCount(args, 1, 1); err != nil {
		return nil, err
	}

	switch x := args[0].(type) {
	case *object.IntObj:
		return &object.FloatObj{Value: float64(x.Value)}, nil

	case *object.StringObj:
		v, err := strconv.ParseFloat(x.Value, 64)
		if err != nil {
			return nil, err
		}

		return &object.FloatObj{Value: v}, nil

	case *object.FloatObj:
		return x, nil

	case *object.BoolObj:
		if x.Value {
			return &object.FloatObj{Value: float64(1.0)}, nil
		} else {
			return &object.FloatObj{Value: float64(0.0)}, nil
		}

	case *object.UndefinedObj:
		return x, nil

	default:
		return &object.UndefinedObj{}, nil
	}
}

// string
func builtin_string(args []object.Object) (interface{}, error) {
	if err := builtinArgCount(args, 1, 1); err != nil {
		return nil, err
	}

	switch x := args[0].(type) {
	case *object.IntObj:
		return &object.StringObj{Value: strconv.FormatInt(x.Value, 10)}, nil

	case *object.StringObj:
		return x, nil

	case *object.FloatObj:
		return &object.StringObj{
			Value: strconv.FormatFloat(x.Value, 'f', -1, 64),
		}, nil

	case *object.BoolObj:
		return &object.StringObj{
			Value: strconv.FormatBool(x.Value),
		}, nil

	case *object.UndefinedObj:
		return x, nil

	default:
		return &object.UndefinedObj{}, nil
	}
}

// bool
func builtin_bool(args []object.Object) (interface{}, error) {
	if err := builtinArgCount(args, 1, 1); err != nil {
		return nil, err
	}

	switch x := args[0].(type) {
	case *object.StringObj:
		b, err := strconv.ParseBool(x.Value)
		if err != nil {
			return nil, err
		}
		return object.Bool(b), nil

	case *object.IntObj:
		if x.Value != 0 {
			return object.True, nil
		} else {
			return object.False, nil
		}

	case *object.FloatObj:
		if x.Value != 0.0 {
			return object.True, nil
		} else {
			return object.False, nil
		}

	case *object.BoolObj:
		return x, nil

	case *object.UndefinedObj:
		return x, nil

	default:
		return &object.UndefinedObj{}, nil
	}
}

func builtinArgCount(args []object.Object, min, max int) error {
	// Exact case
	if min == max {
		if len(args) != min {
			return fmt.Errorf(
				"invalid number of arguments, expected %d, got %d",
				min, len(args))
		}

		return nil
	}

	// Range case
	switch {
	case len(args) < min:
		return fmt.Errorf(
			"too few arguments, min %d, got %d",
			min, len(args))
	case len(args) > max:
		return fmt.Errorf(
			"too many arguments, max %d, got %d",
			max, len(args))
	}

	return nil
}
