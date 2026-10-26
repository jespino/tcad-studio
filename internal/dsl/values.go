package dsl

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Values are float64, string, bool or []any (lists of values).

func typeName(v any) string {
	switch v.(type) {
	case float64:
		return "number"
	case string:
		return "string"
	case bool:
		return "bool"
	case []any:
		return "list"
	}
	return fmt.Sprintf("%T", v)
}

func formatValue(v any) string {
	switch x := v.(type) {
	case float64:
		if math.Abs(x) < 1e-9 {
			return "0"
		}
		return strconv.FormatFloat(x, 'g', 10, 64)
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case []any:
		parts := make([]string, len(x))
		for i, it := range x {
			if s, ok := it.(string); ok {
				parts[i] = strconv.Quote(s)
			} else {
				parts[i] = formatValue(it)
			}
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return fmt.Sprint(v)
}

// FormatLiteral writes a value as TCAD source: strings are quoted.
func FormatLiteral(v any) string {
	switch x := v.(type) {
	case string:
		var b strings.Builder
		b.WriteByte('"')
		for _, r := range x {
			switch r {
			case '"':
				b.WriteString(`\"`)
			case '\\':
				b.WriteString(`\\`)
			case '\n':
				b.WriteString(`\n`)
			case '\t':
				b.WriteString(`\t`)
			default:
				b.WriteRune(r)
			}
		}
		b.WriteByte('"')
		return b.String()
	case []any:
		parts := make([]string, len(x))
		for i, it := range x {
			parts[i] = FormatLiteral(it)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	return formatValue(v)
}

func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	}
	return false
}

func equal(a, b any) bool {
	switch x := a.(type) {
	case float64:
		y, ok := b.(float64)
		return ok && math.Abs(x-y) < 1e-12
	case string:
		y, ok := b.(string)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equal(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	return false
}

func deg2rad(d float64) float64 { return d * math.Pi / 180 }

// snap removes floating point noise so that cos(90) is exactly 0.
func snap(x float64) float64 {
	if r := math.Round(x); math.Abs(x-r) < 1e-12 {
		return r
	}
	return x
}
func rad2deg(r float64) float64 { return r * 180 / math.Pi }

type builtin struct {
	minArgs, maxArgs int
	fn               func(args []any) (any, error)
}

func num1(f func(float64) float64) builtin {
	return builtin{1, 1, func(a []any) (any, error) {
		x, ok := a[0].(float64)
		if !ok {
			return nil, fmt.Errorf("expected a number, got %s", typeName(a[0]))
		}
		return f(x), nil
	}}
}

func nums(a []any) ([]float64, error) {
	// min(list) and max(list) are accepted as well as min(a, b, ...).
	if len(a) == 1 {
		if l, ok := a[0].([]any); ok {
			a = l
		}
	}
	out := make([]float64, len(a))
	for i, v := range a {
		x, ok := v.(float64)
		if !ok {
			return nil, fmt.Errorf("expected numbers, got %s", typeName(v))
		}
		out[i] = x
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("expected at least one number")
	}
	return out, nil
}

var builtins = map[string]builtin{
	"sin":   num1(func(x float64) float64 { return snap(math.Sin(deg2rad(x))) }),
	"cos":   num1(func(x float64) float64 { return snap(math.Cos(deg2rad(x))) }),
	"tan":   num1(func(x float64) float64 { return snap(math.Tan(deg2rad(x))) }),
	"asin":  num1(func(x float64) float64 { return rad2deg(math.Asin(x)) }),
	"acos":  num1(func(x float64) float64 { return rad2deg(math.Acos(x)) }),
	"atan":  num1(func(x float64) float64 { return rad2deg(math.Atan(x)) }),
	"sqrt":  num1(math.Sqrt),
	"abs":   num1(math.Abs),
	"floor": num1(math.Floor),
	"ceil":  num1(math.Ceil),
	"round": num1(math.Round),
	"sign": num1(func(x float64) float64 {
		switch {
		case x > 0:
			return 1
		case x < 0:
			return -1
		}
		return 0
	}),
	"exp": num1(math.Exp),
	"ln":  num1(math.Log),
	"atan2": {2, 2, func(a []any) (any, error) {
		n, err := nums(a)
		if err != nil {
			return nil, err
		}
		return rad2deg(math.Atan2(n[0], n[1])), nil
	}},
	"pow": {2, 2, func(a []any) (any, error) {
		n, err := nums(a)
		if err != nil {
			return nil, err
		}
		return math.Pow(n[0], n[1]), nil
	}},
	"min": {1, -1, func(a []any) (any, error) {
		n, err := nums(a)
		if err != nil {
			return nil, err
		}
		m := n[0]
		for _, x := range n[1:] {
			m = math.Min(m, x)
		}
		return m, nil
	}},
	"max": {1, -1, func(a []any) (any, error) {
		n, err := nums(a)
		if err != nil {
			return nil, err
		}
		m := n[0]
		for _, x := range n[1:] {
			m = math.Max(m, x)
		}
		return m, nil
	}},
	"len": {1, 1, func(a []any) (any, error) {
		switch x := a[0].(type) {
		case []any:
			return float64(len(x)), nil
		case string:
			return float64(len([]rune(x))), nil
		}
		return nil, fmt.Errorf("len expects a list or a string")
	}},
	"str": {1, -1, func(a []any) (any, error) {
		var sb strings.Builder
		for _, v := range a {
			sb.WriteString(formatValue(v))
		}
		return sb.String(), nil
	}},
	"range": {1, 3, func(a []any) (any, error) {
		n, err := nums(a)
		if err != nil {
			return nil, err
		}
		start, end, step := 0.0, n[0], 1.0
		if len(n) >= 2 {
			start, end = n[0], n[1]
		}
		if len(n) == 3 {
			step = n[2]
		}
		if step == 0 {
			return nil, fmt.Errorf("range step cannot be 0")
		}
		var out []any
		for x := start; (step > 0 && x < end-1e-9) || (step < 0 && x > end+1e-9); x += step {
			out = append(out, x)
			if len(out) > 100000 {
				return nil, fmt.Errorf("range is too large")
			}
		}
		return out, nil
	}},
}
