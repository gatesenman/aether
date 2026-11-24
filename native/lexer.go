package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

var keywords = map[string]bool{}

func init() {
	for _, k := range strings.Fields("module import entity view action fn let var return if else while for in match true false nil and or not try catch throw type spawn await flow step requires ensures invariant test example allow") {
		keywords[k] = true
	}
}

type AeError struct {
	Code, Msg string
	Line, Col int
}

func (e *AeError) Error() string { return e.Msg }
func aeErr(code string, line int, f string, a ...any) *AeError {
	return &AeError{Code: code, Msg: fmt.Sprintf(f, a...), Line: line, Col: 1}
}
func (e *AeError) JSON() map[string]any {
	return map[string]any{"severity": "error", "code": e.Code, "message": e.Msg, "line": e.Line, "col": e.Col}
}

// The sole magnitude just outside positive Int64 is retained until the parser
// can see whether it belongs to unary minus (the valid minimum signed integer).
type minIntMagnitude struct{}

type Tok struct {
	Kind                string // num str id kw op eof
	Val                 any
	S                   string
	Line, Col, Pos, End int
}

var ops3 = []string{"|>", "=>", "->", "==", "!=", "<=", ">=", "+=", "-="}

func isIdStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) && r < 128 || (r >= 0x4e00 && r <= 0x9fff)
}
func isIdPart(r rune) bool { return isIdStart(r) || (r >= '0' && r <= '9') }

func Lex(src string) ([]Tok, error) {
	rs := []rune(src)
	// we track byte positions for spans
	bpos := make([]int, len(rs)+1)
	b := 0
	for i, r := range rs {
		bpos[i] = b
		b += len(string(r))
	}
	bpos[len(rs)] = b
	var toks []Tok
	i, line, ls := 0, 1, 0
	for i < len(rs) {
		c := rs[i]
		col := i - ls + 1
		switch {
		case c == '\n':
			line++
			i++
			ls = i
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '/' && i+1 < len(rs) && rs[i+1] == '/':
			for i < len(rs) && rs[i] != '\n' {
				i++
			}
		case c >= '0' && c <= '9':
			j := i
			for j < len(rs) && rs[j] >= '0' && rs[j] <= '9' {
				j++
			}
			isF := false
			if j+1 < len(rs) && rs[j] == '.' && rs[j+1] >= '0' && rs[j+1] <= '9' {
				isF = true
				j++
				for j < len(rs) && rs[j] >= '0' && rs[j] <= '9' {
					j++
				}
			}
			s := string(rs[i:j])
			var v any
			if isF {
				f, err := strconv.ParseFloat(s, 64)
				if err != nil {
					return nil, &AeError{"E001", "numeric literal out of range", line, col}
				}
				v = f
			} else {
				n, err := strconv.ParseInt(s, 10, 64)
				if err != nil {
					if s != "9223372036854775808" {
						return nil, &AeError{"E001", "integer literal out of range", line, col}
					}
					v = minIntMagnitude{}
				} else {
					v = n
				}
			}
			toks = append(toks, Tok{"num", v, s, line, col, bpos[i], bpos[j]})
			i = j
		case c == '"':
			j := i + 1
			var sb strings.Builder
			for j < len(rs) && rs[j] != '"' {
				if rs[j] == '\\' && j+1 < len(rs) {
					e := rs[j+1]
					switch e {
					case 'n':
						sb.WriteRune('\n')
					case 't':
						sb.WriteRune('\t')
					case '{':
						sb.WriteString("\x00LB")
					case '}':
						sb.WriteString("\x00RB")
					default:
						sb.WriteRune(e)
					}
					j += 2
					continue
				}
				if rs[j] == '\n' {
					line++
					ls = j + 1
				}
				sb.WriteRune(rs[j])
				j++
			}
			if j >= len(rs) {
				return nil, aeErr("E001", line, "unterminated string")
			}
			toks = append(toks, Tok{"str", sb.String(), string(rs[i : j+1]), line, col, bpos[i], bpos[j+1]})
			i = j + 1
		case isIdStart(c):
			j := i
			for j < len(rs) && isIdPart(rs[j]) {
				j++
			}
			s := string(rs[i:j])
			k := "id"
			if keywords[s] {
				k = "kw"
			}
			toks = append(toks, Tok{k, s, s, line, col, bpos[i], bpos[j]})
			i = j
		default:
			matched := ""
			if i+1 < len(rs) {
				two := string(rs[i : i+2])
				for _, o := range ops3 {
					if o == two {
						matched = two
					}
				}
			}
			if matched == "" {
				if strings.ContainsRune("-+*/%<>=!(){}[],.:;?|", c) {
					matched = string(c)
				} else {
					return nil, &AeError{"E001", fmt.Sprintf("unexpected character %q", string(c)), line, col}
				}
			}
			n := len([]rune(matched))
			toks = append(toks, Tok{"op", matched, matched, line, col, bpos[i], bpos[i+n]})
			i += n
		}
	}
	toks = append(toks, Tok{"eof", nil, "", line, i - ls + 1, len(src), len(src)})
	return toks, nil
}
