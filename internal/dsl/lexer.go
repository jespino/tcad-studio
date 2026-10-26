// Package dsl implements the TCAD language: a small declarative language to
// describe 3D models by combining solids and holes.
package dsl

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type tokenKind int

const (
	tEOF tokenKind = iota
	tIdent
	tNumber
	tString
	tPunct
)

type Pos struct {
	Line, Col int
}

func (p Pos) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Col) }

type token struct {
	kind tokenKind
	text string
	num  float64
	pos  Pos
}

func (t token) String() string {
	switch t.kind {
	case tEOF:
		return "end of file"
	case tString:
		return strconv.Quote(t.text)
	}
	return fmt.Sprintf("%q", t.text)
}

// Error is a positioned error in a TCAD source file.
type Error struct {
	File string
	Pos  Pos
	Msg  string
}

func (e *Error) Error() string {
	if e.File != "" {
		return fmt.Sprintf("%s:%s: %s", e.File, e.Pos, e.Msg)
	}
	return fmt.Sprintf("%s: %s", e.Pos, e.Msg)
}

var punctuation = []string{
	"&&", "||", "==", "!=", "<=", ">=",
	"(", ")", "{", "}", "[", "]", ",", ".", "=", "<", ">",
	"+", "-", "*", "/", "%", "!", "?", ":", ";",
}

func lex(file, src string) ([]token, error) {
	var toks []token
	line, col := 1, 1
	i := 0
	rs := []rune(src)
	errAt := func(p Pos, f string, a ...any) error {
		return &Error{File: file, Pos: p, Msg: fmt.Sprintf(f, a...)}
	}
	advance := func(n int) {
		for k := 0; k < n; k++ {
			if rs[i] == '\n' {
				line++
				col = 1
			} else {
				col++
			}
			i++
		}
	}
	for i < len(rs) {
		c := rs[i]
		pos := Pos{line, col}
		switch {
		case c == '\n' || unicode.IsSpace(c):
			advance(1)
		case c == '#' || (c == '/' && i+1 < len(rs) && rs[i+1] == '/'):
			for i < len(rs) && rs[i] != '\n' {
				advance(1)
			}
		case c == '/' && i+1 < len(rs) && rs[i+1] == '*':
			advance(2)
			for i < len(rs) && !(rs[i] == '*' && i+1 < len(rs) && rs[i+1] == '/') {
				advance(1)
			}
			if i >= len(rs) {
				return nil, errAt(pos, "unterminated comment")
			}
			advance(2)
		case unicode.IsLetter(c) || c == '_' || c == '$':
			start := i
			for i < len(rs) && (unicode.IsLetter(rs[i]) || unicode.IsDigit(rs[i]) || rs[i] == '_' || (i == start && rs[i] == '$')) {
				advance(1)
			}
			toks = append(toks, token{kind: tIdent, text: string(rs[start:i]), pos: pos})
		case unicode.IsDigit(c) || (c == '.' && i+1 < len(rs) && unicode.IsDigit(rs[i+1])):
			start := i
			for i < len(rs) && (unicode.IsDigit(rs[i]) || rs[i] == '.') {
				advance(1)
			}
			if i < len(rs) && (rs[i] == 'e' || rs[i] == 'E') {
				j := i + 1
				if j < len(rs) && (rs[j] == '+' || rs[j] == '-') {
					j++
				}
				if j < len(rs) && unicode.IsDigit(rs[j]) {
					advance(j - i)
					for i < len(rs) && unicode.IsDigit(rs[i]) {
						advance(1)
					}
				}
			}
			text := string(rs[start:i])
			v, err := strconv.ParseFloat(text, 64)
			if err != nil {
				return nil, errAt(pos, "invalid number %q", text)
			}
			toks = append(toks, token{kind: tNumber, text: text, num: v, pos: pos})
		case c == '"':
			advance(1)
			var sb strings.Builder
			for {
				if i >= len(rs) || rs[i] == '\n' {
					return nil, errAt(pos, "unterminated string")
				}
				if rs[i] == '"' {
					advance(1)
					break
				}
				if rs[i] == '\\' && i+1 < len(rs) {
					switch rs[i+1] {
					case 'n':
						sb.WriteRune('\n')
					case 't':
						sb.WriteRune('\t')
					case '"':
						sb.WriteRune('"')
					case '\\':
						sb.WriteRune('\\')
					default:
						return nil, errAt(Pos{line, col}, "unknown escape \\%c", rs[i+1])
					}
					advance(2)
					continue
				}
				sb.WriteRune(rs[i])
				advance(1)
			}
			toks = append(toks, token{kind: tString, text: sb.String(), pos: pos})
		default:
			matched := false
			for _, p := range punctuation {
				if strings.HasPrefix(string(rs[i:min(i+len(p), len(rs))]), p) {
					toks = append(toks, token{kind: tPunct, text: p, pos: pos})
					advance(len(p))
					matched = true
					break
				}
			}
			if !matched {
				return nil, errAt(pos, "unexpected character %q", c)
			}
		}
	}
	toks = append(toks, token{kind: tEOF, pos: Pos{line, col}})
	return toks, nil
}
