package dsl

import (
	"regexp"
	"strconv"
	"strings"
)

// ParamAnnotation is the optional metadata written in the comment after a
// param, in the style of the OpenSCAD customizer. Ranges are only hints for
// the viewer's sliders; a closed set of values is part of the language
// instead (param font = "sans" in ["sans", "mono"]), because it is
// enforced.
//
//	param width = 60        // [20:200] Outer width
//	param wall = 2          // [1:0.5:5]  (min:step:max)
//	param lid = true        // Also print the lid
type ParamAnnotation struct {
	Doc  string   `json:"doc,omitempty"`
	Min  *float64 `json:"min,omitempty"`
	Max  *float64 `json:"max,omitempty"`
	Step *float64 `json:"step,omitempty"`
}

var rangeRe = regexp.MustCompile(`^\[\s*(-?[\d.]+)\s*:\s*(-?[\d.]+)\s*(?::\s*(-?[\d.]+)\s*)?\]`)

// parseAnnotation reads the comment at the end of a source line.
func parseAnnotation(line string) ParamAnnotation {
	var a ParamAnnotation
	c, ok := lineComment(line)
	if !ok {
		return a
	}
	c = strings.TrimSpace(c)
	if m := rangeRe.FindStringSubmatch(c); m != nil {
		nums := []float64{}
		for _, s := range m[1:] {
			if s == "" {
				continue
			}
			f, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return ParamAnnotation{Doc: c}
			}
			nums = append(nums, f)
		}
		if len(nums) == 2 {
			a.Min, a.Max = &nums[0], &nums[1]
		} else {
			a.Min, a.Step, a.Max = &nums[0], &nums[1], &nums[2]
		}
		c = strings.TrimSpace(c[len(m[0]):])
	}
	a.Doc = c
	return a
}

// lineComment returns the text after // or # outside of strings.
func lineComment(line string) (string, bool) {
	inStr := false
	for i := 0; i < len(line); i++ {
		switch ch := line[i]; {
		case inStr && ch == '\\':
			i++
		case ch == '"':
			inStr = !inStr
		case !inStr && ch == '#':
			return line[i+1:], true
		case !inStr && ch == '/' && i+1 < len(line) && line[i+1] == '/':
			return line[i+2:], true
		}
	}
	return "", false
}
