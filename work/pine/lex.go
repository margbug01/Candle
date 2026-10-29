// Package pine runs indicators written in a subset of TradingView's Pine
// Script (versions 4 to 6): the declarations, inputs, operators, if, for,
// while and switch, functions of the script's own, history references and
// most of the ta, math and color functions, and their plots, levels,
// fills, backgrounds and shapes. What Candle cannot draw (labels, lines,
// boxes, tables) is skipped; what it cannot work out (other symbols,
// strategies, arrays) is an error on the line that asks for it.
//
// A script runs once on every bar, from the oldest on, as on TradingView:
// each variable, and each call of a ta function, keeps its values by bar,
// so that running the newest bar again, as it changes, gives what running
// every bar would.
package pine

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokKind uint8

const (
	tkIdent tokKind = iota // names, keywords and qualified names as ta.sma
	tkNum
	tkStr
	tkColor // #RRGGBB or #RRGGBBAA
	tkOp
)

type token struct {
	kind       tokKind
	text       string // an identifier's or operator's text, a string's value
	num        float64
	start, end int // byte offsets in the source
}

// A line is a logical line: its indentation level and its tokens, physical
// lines continued onto it joined.
type line struct {
	level int
	toks  []token
}

// Error is a problem with a script, at bytes Start to End of its source.
type Error struct {
	Start, End int
	Msg        string
}

func (e *Error) Error() string { return e.Msg }

func errAt(t token, format string, args ...any) *Error {
	return &Error{Start: t.start, End: t.end, Msg: fmt.Sprintf(format, args...)}
}

// Line is the line, from 1, that byte offset off of src is on.
func Line(src string, off int) int {
	off = min(max(off, 0), len(src))
	return strings.Count(src[:off], "\n") + 1
}

var ops3 = []string{"..."}
var ops2 = []string{":=", "+=", "-=", "*=", "/=", "%=", "==", "!=", "<=", ">=", "=>"}

const ops1 = "+-*/%<>=?:,()[].!"

// lines splits src into logical lines. A physical line goes on the one
// before it while brackets are open, when it ends with an operator or a
// comma, or when it is indented by a number of spaces that is not a whole
// number of levels (four spaces, or a tab).
func lines(src string) ([]line, error) {
	var out []line
	depth := 0
	off := 0
	for _, phys := range strings.SplitAfter(src, "\n") {
		start := off
		off += len(phys)
		text := strings.TrimRight(phys, "\r\n")
		indent, i := 0, 0
		for ; i < len(text); i++ {
			if text[i] == ' ' {
				indent++
			} else if text[i] == '\t' {
				indent += 4
			} else {
				break
			}
		}
		toks, err := tokenize(text[i:], start+i)
		if err != nil {
			return nil, err
		}
		if len(toks) == 0 {
			continue
		}
		n := len(out)
		if n > 0 && (depth > 0 || indent%4 != 0 || continues(out[n-1].toks)) {
			out[n-1].toks = append(out[n-1].toks, toks...)
		} else {
			out = append(out, line{level: indent / 4, toks: toks})
		}
		for _, t := range toks {
			if t.kind == tkOp {
				switch t.text {
				case "(", "[":
					depth++
				case ")", "]":
					depth = max(depth-1, 0)
				}
			}
		}
	}
	return out, nil
}

// continues reports whether a line ending with toks goes on on the next.
func continues(toks []token) bool {
	t := toks[len(toks)-1]
	if t.kind == tkIdent {
		return t.text == "and" || t.text == "or" || t.text == "not"
	}
	if t.kind != tkOp {
		return false
	}
	switch t.text {
	case ",", "+", "-", "*", "/", "%", "?", ":", "==", "!=", "<", ">", "<=", ">=", "(", "[":
		return true
	}
	return false
}

// tokenize splits one physical line, which starts at byte base of the
// source, into tokens, up to a comment.
func tokenize(s string, base int) ([]token, error) {
	var toks []token
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case strings.HasPrefix(s[i:], "//"):
			return toks, nil
		case c == '"' || c == '\'':
			j := i + 1
			var b strings.Builder
			for ; j < len(s) && s[j] != c; j++ {
				if s[j] == '\\' && j+1 < len(s) {
					j++
					switch s[j] {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					default:
						b.WriteByte(s[j])
					}
					continue
				}
				b.WriteByte(s[j])
			}
			if j >= len(s) {
				return nil, &Error{Start: base + i, End: base + len(s), Msg: "字符串没有结束的引号"}
			}
			toks = append(toks, token{kind: tkStr, text: b.String(), start: base + i, end: base + j + 1})
			i = j + 1
		case c == '#':
			j := i + 1
			for j < len(s) && isHex(s[j]) {
				j++
			}
			if n := j - i - 1; n != 6 && n != 8 {
				return nil, &Error{Start: base + i, End: base + j, Msg: "颜色要写成 #RRGGBB 或 #RRGGBBAA"}
			}
			toks = append(toks, token{kind: tkColor, text: s[i:j], start: base + i, end: base + j})
			i = j
		case c >= '0' && c <= '9' || c == '.' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9':
			j := i
			for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == '.') {
				j++
			}
			if j < len(s) && (s[j] == 'e' || s[j] == 'E') {
				k := j + 1
				if k < len(s) && (s[k] == '+' || s[k] == '-') {
					k++
				}
				if k < len(s) && s[k] >= '0' && s[k] <= '9' {
					j = k
					for j < len(s) && s[j] >= '0' && s[j] <= '9' {
						j++
					}
				}
			}
			var v float64
			if _, err := fmt.Sscan(s[i:j], &v); err != nil {
				return nil, &Error{Start: base + i, End: base + j, Msg: "看不懂的数字 " + s[i:j]}
			}
			toks = append(toks, token{kind: tkNum, num: v, text: s[i:j], start: base + i, end: base + j})
			i = j
		case isIdentStart(s[i:]):
			j := i
			for j < len(s) {
				r, n := utf8.DecodeRuneInString(s[j:])
				if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
					j += n
					continue
				}
				// A dot followed by a name goes on the name: ta.sma, color.red.
				if r == '.' && j+1 < len(s) && isIdentStart(s[j+1:]) {
					j++
					continue
				}
				break
			}
			toks = append(toks, token{kind: tkIdent, text: s[i:j], start: base + i, end: base + j})
			i = j
		default:
			op := ""
			for _, o := range ops3 {
				if strings.HasPrefix(s[i:], o) {
					op = o
				}
			}
			if op == "" {
				for _, o := range ops2 {
					if strings.HasPrefix(s[i:], o) {
						op = o
						break
					}
				}
			}
			if op == "" && strings.IndexByte(ops1, c) >= 0 {
				op = s[i : i+1]
			}
			if op == "" {
				r, n := utf8.DecodeRuneInString(s[i:])
				return nil, &Error{Start: base + i, End: base + i + n, Msg: fmt.Sprintf("看不懂的符号 %q", r)}
			}
			toks = append(toks, token{kind: tkOp, text: op, start: base + i, end: base + i + len(op)})
			i += len(op)
		}
	}
	return toks, nil
}

func isHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

func isIdentStart(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return r == '_' || unicode.IsLetter(r)
}
