package decimal

import (
	"fmt"
	"math"
	"strconv"

	"github.com/cockroachdb/apd"
	sdk "github.com/hashicorp/sentinel-sdk"
	"github.com/hashicorp/sentinel-sdk/framework"
	"github.com/mitchellh/mapstructure"
)

const decimalPrecision = 100

type rawDecimal struct {
	// Reference is the string representation of the decimal.
	Reference string `mapstructure:"string"`
}

func parseDecimalInput(ctx *apd.Context, in interface{}) (*apd.Decimal, error) {
	d := new(apd.Decimal)
	if in == nil {
		return d, fmt.Errorf("nil input to decimal parser")
	}

	switch t := in.(type) {
	case string:
		d, _, err := ctx.NewFromString(t)
		if err != nil {
			return d, fmt.Errorf("unable to parse decimal value: %s", err.Error())
		}
		return d, nil

	case *apd.Decimal:
		return t, nil

	case int64:
		return d.SetInt64(t), nil

	case float64:
		_, err := d.SetFloat64(t)
		return d, err

	case map[string]interface{}, map[string]string:
		// Handle the case where we're constructing a decimal from another
		// decimal.
		var raw rawDecimal
		if err := mapstructure.Decode(in, &raw); err != nil {
			return d, err
		}
		if raw.Reference == "" {
			return d, fmt.Errorf("unexpected blank value for 'string'")
		}
		d, _, err := ctx.NewFromString(raw.Reference)
		if err != nil {
			return d, fmt.Errorf("unable to parse decimal value: %s", err.Error())
		}
		return d, nil
	}

	return d, fmt.Errorf("unknown decimal format: %T", in)
}

// New creates a new Import.
func New() sdk.Import {
	return &framework.Import{
		Root: &root{},
	}
}

type root struct {
	ctx *apd.Context
}

// framework.Root impl.
func (m *root) Configure(raw map[string]interface{}) error {
	m.ctx = apd.BaseContext.WithPrecision(decimalPrecision)
	m.ctx.Traps &^= apd.InvalidOperation
	return nil
}

// framework.NamespaceCreator impl.
func (m *root) Namespace() framework.Namespace {
	return &rootNamespace{m.ctx}
}

// rootNamespace is the root-level namespace.
type rootNamespace struct {
	ctx *apd.Context
}

// framework.New impl.
func (r *rootNamespace) New(data map[string]interface{}) (framework.Namespace, error) {
	val, err := parseDecimalInput(r.ctx, data)
	if err != nil {
		return nil, err
	}

	return &fixedDecimal{
		ctx:   r.ctx,
		fixed: val,
	}, nil
}

// framework.Namespace impl.
func (r *rootNamespace) Get(key string) (interface{}, error) {
	switch key {
	case "nan", "NaN":
		return &fixedDecimal{
			ctx:   r.ctx,
			fixed: &apd.Decimal{Form: apd.NaN},
		}, nil
	}

	return nil, fmt.Errorf("unsupported field: %s", key)
}

// framework.Call impl.
func (r *rootNamespace) Func(key string) interface{} {
	switch key {
	case "new":
		return func(in interface{}) (interface{}, error) {
			val, err := parseDecimalInput(r.ctx, in)
			if err != nil {
				return nil, err
			}

			return &fixedDecimal{
				ctx:   r.ctx,
				fixed: val,
			}, nil
		}
	case "isnan":
		return func(in interface{}) (interface{}, error) {
			val, err := parseDecimalInput(r.ctx, in)
			if err != nil {
				return nil, err
			}
			return val.Form&(apd.NaNSignaling|apd.NaN) != 0, nil
		}
	case "isinf", "isinfinite":
		return func(in interface{}, sign int) (interface{}, error) {
			val, err := parseDecimalInput(r.ctx, in)
			if err != nil {
				return nil, err
			}
			negative := sign < 0
			return val.Form == apd.Infinite && val.Negative == negative, nil
		}
	case "inf", "infinity":
		return func(in int) (interface{}, error) {
			return &fixedDecimal{
				ctx: r.ctx,
				fixed: &apd.Decimal{
					Negative: in < 0,
					Form:     apd.Infinite,
				},
			}, nil
		}
	}
	return nil
}

// Holds a fixed decimal. Created via the `new` function.
type fixedDecimal struct {
	fixed   *apd.Decimal
	ctx     *apd.Context
	mapData map[string]interface{}
}

// framework.Call impl.
func (f *fixedDecimal) Func(key string) interface{} {
	switch key {
	case "is":
		return f.cmpImpl(func(c int64) bool {
			return c == 0
		})
	case "is_not":
		return f.cmpImpl(func(c int64) bool {
			return c != 0
		})
	case "lt", "less_than":
		return f.cmpImpl(func(c int64) bool {
			return c < 0
		})
	case "lte", "less_than_or_equals":
		return f.cmpImpl(func(c int64) bool {
			return c <= 0
		})
	case "gt", "greater_than":
		return f.cmpImpl(func(c int64) bool {
			return c > 0
		})
	case "gte", "greater_than_or_equals":
		return f.cmpImpl(func(c int64) bool {
			return c >= 0
		})
	case "add":
		return func(in interface{}) (interface{}, error) {
			val, err := parseDecimalInput(f.ctx, in)
			if err != nil {
				return nil, err
			}
			_, err = f.ctx.Add(val, f.fixed, val)
			if err != nil {
				return nil, err
			}
			return &fixedDecimal{ctx: f.ctx, fixed: val}, nil
		}
	case "sub", "subtract":
		return func(in interface{}) (interface{}, error) {
			val, err := parseDecimalInput(f.ctx, in)
			if err != nil {
				return nil, err
			}
			_, err = f.ctx.Sub(val, f.fixed, val)
			if err != nil {
				return nil, err
			}
			return &fixedDecimal{ctx: f.ctx, fixed: val}, nil
		}
	case "mul", "multiply":
		return func(in interface{}) (interface{}, error) {
			val, err := parseDecimalInput(f.ctx, in)
			if err != nil {
				return nil, err
			}
			_, err = f.ctx.Mul(val, f.fixed, val)
			if err != nil {
				return nil, err
			}
			return &fixedDecimal{ctx: f.ctx, fixed: val}, nil
		}
	case "div", "divide":
		return func(in interface{}) (interface{}, error) {
			val, err := parseDecimalInput(f.ctx, in)
			if err != nil {
				return nil, err
			}

			if val.IsZero() {
				return nil, fmt.Errorf("divide by zero not allowed")
			}

			_, err = f.ctx.Quo(val, f.fixed, val)
			if err != nil {
				return nil, err
			}
			return &fixedDecimal{ctx: f.ctx, fixed: val}, nil
		}
	case "mod", "modulo", "rem", "remainder":
		return func(in interface{}) (interface{}, error) {
			val, err := parseDecimalInput(f.ctx, in)
			if err != nil {
				return nil, err
			}

			if val.IsZero() {
				return nil, fmt.Errorf("modulo by zero not allowed")
			}

			_, err = f.ctx.Rem(val, f.fixed, val)
			if err != nil {
				return nil, err
			}
			return &fixedDecimal{ctx: f.ctx, fixed: val}, nil
		}
	case "pow", "power":
		return func(in interface{}) (interface{}, error) {
			val, err := parseDecimalInput(f.ctx, in)
			if err != nil {
				return nil, err
			}
			_, err = f.ctx.Pow(val, f.fixed, val)
			if err != nil {
				return nil, err
			}
			return &fixedDecimal{ctx: f.ctx, fixed: val}, nil
		}
	case "exp":
		return func() (interface{}, error) {
			d := new(apd.Decimal)
			_, err := f.ctx.Exp(d, f.fixed)
			if err != nil {
				return nil, err
			}
			return &fixedDecimal{ctx: f.ctx, fixed: d}, nil
		}
	case "ln", "loge":
		return func() (interface{}, error) {
			d := new(apd.Decimal)
			_, err := f.ctx.Ln(d, f.fixed)
			if err != nil {
				return nil, err
			}
			return &fixedDecimal{ctx: f.ctx, fixed: d}, nil
		}
	case "log", "log10":
		return func() (interface{}, error) {
			d := new(apd.Decimal)
			_, err := f.ctx.Log10(d, f.fixed)
			if err != nil {
				return nil, err
			}
			return &fixedDecimal{ctx: f.ctx, fixed: d}, nil
		}
	case "sqrt", "square_root":
		return func() (interface{}, error) {
			d := new(apd.Decimal)
			_, err := f.ctx.Sqrt(d, f.fixed)
			if err != nil {
				return nil, err
			}
			return &fixedDecimal{ctx: f.ctx, fixed: d}, nil
		}
	case "ceil", "ceiling":
		return func() (interface{}, error) {
			d := new(apd.Decimal)
			_, err := f.ctx.Ceil(d, f.fixed)
			if err != nil {
				return nil, err
			}
			return &fixedDecimal{ctx: f.ctx, fixed: d}, nil
		}
	case "floor":
		return func() (interface{}, error) {
			d := new(apd.Decimal)
			_, err := f.ctx.Floor(d, f.fixed)
			if err != nil {
				return nil, err
			}
			return &fixedDecimal{ctx: f.ctx, fixed: d}, nil
		}
	case "abs", "absolute":
		return func() (interface{}, error) {
			d := new(apd.Decimal)
			d.Abs(f.fixed)
			return &fixedDecimal{ctx: f.ctx, fixed: d}, nil
		}
	case "neg", "negate":
		return func() (interface{}, error) {
			d := new(apd.Decimal)
			d.Neg(f.fixed)
			return &fixedDecimal{ctx: f.ctx, fixed: d}, nil
		}
	}
	return nil
}

// framework.Namespace impl.
func (f *fixedDecimal) Get(key string) (interface{}, error) {
	m, err := f.Map()
	if err != nil {
		return nil, err
	}

	if k, ok := m[key]; ok {
		return k, nil
	} else {
		return nil, fmt.Errorf("unsupported field: %s", key)
	}
}

// framework.Map impl.
func (f *fixedDecimal) Map() (map[string]interface{}, error) {
	if f.mapData != nil {
		return f.mapData, nil
	}

	d := new(apd.Decimal)
	mapData := map[string]interface{}{
		"string":      f.fixed.String(),
		"sign":        f.fixed.Sign(),
		"coefficient": f.fixed.Coeff.String(),
		"exponent":    f.fixed.Exponent,
		"float":       sdk.Undefined,
		"int":         sdk.Undefined,
	}

	switch f.fixed.Form {
	case apd.Finite:
		_, err := f.ctx.RoundToIntegralValue(d, f.fixed)
		if err != nil {
			// This will error if any rounding traps have been set. Inexact and
			// Rounded flags are ignored.
			return nil, fmt.Errorf("rounding error: %s", err)
		}
		// An error here usually means our number is outside the range of an
		// int64, so we can leave the default undefined value.
		if d, err := d.Int64(); err == nil {
			mapData["int"] = d
		}

		if f, err := f.fixed.Float64(); err == nil {
			mapData["float"] = f
		} else {
			// Float64 can return any error strconv.ParseFloat can
			// https://golang.org/pkg/strconv/#ParseFloat
			e, ok := err.(*strconv.NumError)
			if !ok {
				return nil, fmt.Errorf("unknown error: %s", err.Error())
			}

			if e.Err == strconv.ErrSyntax {
				// This should never happen, but let's handle it anyway.
				return nil, fmt.Errorf("syntax error parsing the decimal: %s", err.Error())
			} else if e.Err == strconv.ErrRange {
				// We may overflow the float type, which will return an error and
				// a "±inf" value, which is valid. Let's toss the error and set the
				// result.
				mapData["float"] = f
			} else {
				return nil, fmt.Errorf("unknown error: %s", err.Error())
			}
		}
	case apd.Infinite:
		mapData["float"] = math.Inf(f.fixed.Sign())
	case apd.NaN:
		mapData["float"] = math.NaN()
	}

	f.mapData = mapData
	return mapData, nil
}

// cmpImpl returns a function which compares its argument with the fixed
// decimal. The returned function returns false for cmp(nan, nan). It takes
// a function that receives a value in [-1, 1] indicating if the argument is
// less than, equals to, or greater than the fixed decimal.
func (f *fixedDecimal) cmpImpl(cf func(int64) bool) interface{} {
	return func(in interface{}) (bool, error) {
		val, err := parseDecimalInput(f.ctx, in)
		if err != nil {
			return false, err
		}

		res := new(apd.Decimal)
		_, err = apd.BaseContext.Cmp(res, f.fixed, val)
		if err != nil {
			return false, err
		}

		// A NaN is never equal to itself.
		if res.Form&(apd.NaN|apd.NaNSignaling) != 0 {
			return false, nil
		}

		i, err := res.Int64()
		if err != nil {
			return false, err
		}

		return cf(i), nil
	}
}
