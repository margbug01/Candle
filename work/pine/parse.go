package pine

import (
	"strconv"
	"strings"
)

// The syntax tree.

type expr interface{ at() token }

type (
	numLit   struct{ t token }
	strLit   struct{ t token }
	colorLit struct{ t token }
	boolLit  struct {
		t token
		v bool
	}
	naLit   struct{ t token }
	nameRef struct{ t token } // a variable or a built-in one, as close or ta.tr
	callX   struct {
		t    token // the function's name
		fn   string
		args []argX
	}
	indexX struct {
		t      token
		x, off expr
	}
	unaryX struct {
		t  token
		op string
		x  expr
	}
	binaryX struct {
		t    token
		op   string
		l, r expr
	}
	ternaryX struct {
		t       token
		c, a, b expr
	}
	tupleX struct {
		t  token
		xs []expr
	}
	ifX struct {
		t      token
		conds  []expr
		blocks [][]stmt
		els    []stmt // nil without else
	}
	switchX struct {
		t       token
		subject expr // nil when each case is a condition
		cases   []switchCase
	}
)

type argX struct {
	name string
	x    expr
	t    token
}

type switchCase struct {
	match expr // nil for the default
	body  []stmt
}

func (x *numLit) at() token   { return x.t }
func (x *strLit) at() token   { return x.t }
func (x *colorLit) at() token { return x.t }
func (x *boolLit) at() token  { return x.t }
func (x *naLit) at() token    { return x.t }
func (x *nameRef) at() token  { return x.t }
func (x *callX) at() token    { return x.t }
func (x *indexX) at() token   { return x.t }
func (x *unaryX) at() token   { return x.t }
func (x *binaryX) at() token  { return x.t }
func (x *ternaryX) at() token { return x.t }
func (x *tupleX) at() token   { return x.t }
func (x *ifX) at() token      { return x.t }
func (x *switchX) at() token  { return x.t }

type stmt interface{}

type (
	declS struct {
		t    token
		mode string // "", "var" or "varip"
		typ  string
		name string
		x    expr
	}
	tupleDeclS struct {
		t     token
		names []token
		x     expr
	}
	assignS struct {
		t    token
		name string
		op   string // := += -= *= /= %=
		x    expr
	}
	exprS struct{ x expr }
	forS  struct {
		t            token
		v            string
		from, to, by expr
		body         []stmt
	}
	whileS struct {
		t    token
		cond expr
		body []stmt
	}
	breakS    struct{ t token }
	continueS struct{ t token }
	funcS     struct {
		t      token
		name   string
		params []paramX
		body   []stmt
	}
)

type paramX struct {
	t   token
	def expr
}

// typeWords are what can stand before a variable's name in its declaration.
var typeWords = map[string]bool{"int": true, "float": true, "bool": true, "color": true, "string": true,
	"series": true, "simple": true, "const": true, "input": true,
	"line": true, "label": true, "box": true, "table": true, "linefill": true, "polyline": true, "chart.point": true}

type parser struct {
	lines []line
	li    int
	toks  []token
	ti    int
	end   token // the end of the line being parsed, for errors
}

func parse(src string) ([]stmt, error) {
	ls, err := lines(src)
	if err != nil {
		return nil, err
	}
	p := &parser{lines: ls}
	if len(ls) > 0 && ls[0].level != 0 {
		return nil, errAt(ls[0].toks[0], "第一行不能缩进")
	}
	body, err := p.block(0)
	if err != nil {
		return nil, err
	}
	if p.li < len(p.lines) {
		return nil, errAt(p.lines[p.li].toks[0], "缩进不对")
	}
	return body, nil
}

// block parses the lines at level, up to one less indented.
func (p *parser) block(level int) ([]stmt, error) {
	var out []stmt
	for p.li < len(p.lines) {
		l := p.lines[p.li]
		if l.level < level {
			break
		}
		if l.level > level {
			return nil, errAt(l.toks[0], "缩进不对：这一行比上一行多缩进了，但上一行不是 if、for、函数这类开头")
		}
		s, err := p.stmt(level)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// indented parses the block below the line just parsed, which must have one.
func (p *parser) indented(level int, after token) ([]stmt, error) {
	if p.li >= len(p.lines) || p.lines[p.li].level <= level {
		return nil, errAt(after, "下面缺少缩进的代码块")
	}
	return p.block(level + 1)
}

func (p *parser) startLine() {
	l := p.lines[p.li]
	p.li++
	p.toks, p.ti = l.toks, 0
	last := l.toks[len(l.toks)-1]
	p.end = token{start: last.end, end: last.end + 1}
}

func (p *parser) peek() token {
	if p.ti < len(p.toks) {
		return p.toks[p.ti]
	}
	return token{kind: tkOp, text: "", start: p.end.start, end: p.end.end}
}

func (p *parser) peekAt(k int) token {
	if p.ti+k < len(p.toks) {
		return p.toks[p.ti+k]
	}
	return token{kind: tkOp, start: p.end.start, end: p.end.end}
}

func (p *parser) atEnd() bool { return p.ti >= len(p.toks) }

func (p *parser) next() token {
	t := p.peek()
	if p.ti < len(p.toks) {
		p.ti++
	}
	return t
}

func (p *parser) isOp(s string) bool {
	t := p.peek()
	return t.kind == tkOp && t.text == s && p.ti < len(p.toks)
}

func (p *parser) isWord(s string) bool {
	t := p.peek()
	return t.kind == tkIdent && t.text == s && p.ti < len(p.toks)
}

func (p *parser) expect(s string) (token, error) {
	if !p.isOp(s) {
		t := p.peek()
		if p.atEnd() {
			return t, errAt(t, "这里少了 %s", s)
		}
		return t, errAt(t, "这里应该是 %s，不是 %s", s, t.text)
	}
	return p.next(), nil
}

func (p *parser) lineDone() error {
	if !p.atEnd() {
		t := p.peek()
		return errAt(t, "这一行多出了 %s", describe(t))
	}
	return nil
}

func describe(t token) string {
	switch t.kind {
	case tkStr:
		return strconv.Quote(t.text)
	case tkNum, tkColor:
		return t.text
	}
	return "「" + t.text + "」"
}

var unsupportedWords = map[string]string{
	"import": "Candle 不支持 import 别的库",
	"export": "Candle 不支持 export（库）",
	"type":   "Candle 不支持自定义类型（type）",
	"enum":   "Candle 不支持 enum",
	"method": "Candle 不支持 method",
}

func (p *parser) stmt(level int) (stmt, error) {
	p.startLine()
	t := p.peek()
	if t.kind == tkIdent {
		if msg, ok := unsupportedWords[t.text]; ok {
			return nil, errAt(t, "%s", msg)
		}
		switch t.text {
		case "if", "switch":
			x, err := p.blockExpr(level)
			if err != nil {
				return nil, err
			}
			return &exprS{x}, nil
		case "for":
			return p.forStmt(level)
		case "while":
			p.next()
			cond, err := p.expr()
			if err != nil {
				return nil, err
			}
			if err := p.lineDone(); err != nil {
				return nil, err
			}
			body, err := p.indented(level, t)
			if err != nil {
				return nil, err
			}
			return &whileS{t: t, cond: cond, body: body}, nil
		case "break", "continue":
			p.next()
			if err := p.lineDone(); err != nil {
				return nil, err
			}
			if t.text == "break" {
				return &breakS{t}, nil
			}
			return &continueS{t}, nil
		}
		if p.peekAt(1).kind == tkOp && p.peekAt(1).text == "(" && p.isFuncDef() {
			return p.funcDef(level)
		}
	}
	if p.isOp("[") && p.isTupleDecl() {
		return p.tupleDecl(level)
	}
	// A declaration: [var|varip] [type words] name = …
	save := p.ti
	mode, typ := "", ""
	if p.isWord("var") || p.isWord("varip") {
		mode = p.next().text
	}
	for p.peek().kind == tkIdent && typeWords[p.peek().text] && p.peekAt(1).kind == tkIdent {
		typ = p.next().text
	}
	if p.peek().kind == tkIdent && strings.HasPrefix(p.peek().text, "array") && p.peekAt(1).kind == tkOp && p.peekAt(1).text == "<" {
		return nil, errAt(p.peek(), "Candle 不支持 array")
	}
	if p.peek().kind == tkIdent && p.peekAt(1).kind == tkOp && p.peekAt(1).text == "=" {
		name := p.next()
		p.next()
		x, err := p.valueExpr(level)
		if err != nil {
			return nil, err
		}
		return &declS{t: name, mode: mode, typ: typ, name: name.text, x: x}, nil
	}
	if mode != "" || typ != "" {
		return nil, errAt(p.peek(), "声明变量要写成 名字 = 值")
	}
	p.ti = save
	if p.peek().kind == tkIdent {
		if op := p.peekAt(1); op.kind == tkOp && (op.text == ":=" || len(op.text) == 2 && op.text[1] == '=' && strings.Contains("+-*/%", op.text[:1])) {
			name := p.next()
			p.next()
			x, err := p.valueExpr(level)
			if err != nil {
				return nil, err
			}
			return &assignS{t: name, name: name.text, op: op.text, x: x}, nil
		}
	}
	x, err := p.expr()
	if err != nil {
		return nil, err
	}
	if p.isOp("=") {
		return nil, errAt(p.peek(), "= 的左边只能是变量名")
	}
	if err := p.lineDone(); err != nil {
		return nil, err
	}
	return &exprS{x}, nil
}

// valueExpr is what follows = or :=: an expression, or an if or a switch
// whose blocks follow.
func (p *parser) valueExpr(level int) (expr, error) {
	if p.isWord("if") || p.isWord("switch") {
		return p.blockExpr(level)
	}
	x, err := p.expr()
	if err != nil {
		return nil, err
	}
	return x, p.lineDone()
}

// blockExpr parses an if or a switch at the parser's place in its line,
// which it must end, and the blocks below it.
func (p *parser) blockExpr(level int) (expr, error) {
	t := p.next()
	if t.text == "switch" {
		sw := &switchX{t: t}
		if !p.atEnd() {
			x, err := p.expr()
			if err != nil {
				return nil, err
			}
			sw.subject = x
		}
		if err := p.lineDone(); err != nil {
			return nil, err
		}
		if p.li >= len(p.lines) || p.lines[p.li].level <= level {
			return nil, errAt(t, "switch 下面缺少分支")
		}
		for p.li < len(p.lines) && p.lines[p.li].level == level+1 {
			p.startLine()
			var c switchCase
			if !p.isOp("=>") {
				x, err := p.expr()
				if err != nil {
					return nil, err
				}
				c.match = x
			}
			arrow, err := p.expect("=>")
			if err != nil {
				return nil, err
			}
			if p.atEnd() {
				body, err := p.indented(level+1, arrow)
				if err != nil {
					return nil, err
				}
				c.body = body
			} else {
				x, err := p.valueExpr(level + 1)
				if err != nil {
					return nil, err
				}
				c.body = []stmt{&exprS{x}}
			}
			sw.cases = append(sw.cases, c)
		}
		return sw, nil
	}
	ix := &ifX{t: t}
	for {
		cond, err := p.expr()
		if err != nil {
			return nil, err
		}
		if err := p.lineDone(); err != nil {
			return nil, err
		}
		body, err := p.indented(level, t)
		if err != nil {
			return nil, err
		}
		ix.conds, ix.blocks = append(ix.conds, cond), append(ix.blocks, body)
		if p.li >= len(p.lines) || p.lines[p.li].level != level || p.lines[p.li].toks[0].text != "else" || p.lines[p.li].toks[0].kind != tkIdent {
			return ix, nil
		}
		p.startLine()
		et := p.next()
		if p.isWord("if") {
			t = p.next()
			continue
		}
		if err := p.lineDone(); err != nil {
			return nil, err
		}
		els, err := p.indented(level, et)
		if err != nil {
			return nil, err
		}
		if els == nil {
			els = []stmt{}
		}
		ix.els = els
		return ix, nil
	}
}

func (p *parser) forStmt(level int) (stmt, error) {
	t := p.next()
	if p.isOp("[") {
		return nil, errAt(p.peek(), "Candle 不支持 for … in（数组）")
	}
	v := p.next()
	if v.kind != tkIdent {
		return nil, errAt(v, "for 后面应该是循环变量的名字")
	}
	if p.isWord("in") {
		return nil, errAt(p.peek(), "Candle 不支持 for … in（数组）")
	}
	if _, err := p.expect("="); err != nil {
		return nil, err
	}
	from, err := p.expr()
	if err != nil {
		return nil, err
	}
	if !p.isWord("to") {
		return nil, errAt(p.peek(), "for 循环要写成 for i = 0 to 10")
	}
	p.next()
	to, err := p.expr()
	if err != nil {
		return nil, err
	}
	var by expr
	if p.isWord("by") {
		p.next()
		if by, err = p.expr(); err != nil {
			return nil, err
		}
	}
	if err := p.lineDone(); err != nil {
		return nil, err
	}
	body, err := p.indented(level, t)
	if err != nil {
		return nil, err
	}
	return &forS{t: t, v: v.text, from: from, to: to, by: by, body: body}, nil
}

// isFuncDef reports whether the line, at a name and (, defines a function:
// the brackets close and => follows.
func (p *parser) isFuncDef() bool {
	depth := 0
	for k := p.ti + 1; k < len(p.toks); k++ {
		t := p.toks[k]
		if t.kind != tkOp {
			continue
		}
		switch t.text {
		case "(", "[":
			depth++
		case ")", "]":
			depth--
			if depth == 0 {
				return k+1 < len(p.toks) && p.toks[k+1].kind == tkOp && p.toks[k+1].text == "=>"
			}
		}
	}
	return false
}

func (p *parser) funcDef(level int) (stmt, error) {
	name := p.next()
	p.next() // (
	f := &funcS{t: name, name: name.text}
	for !p.isOp(")") {
		for p.peek().kind == tkIdent && typeWords[p.peek().text] && p.peekAt(1).kind == tkIdent {
			p.next()
		}
		pt := p.next()
		if pt.kind != tkIdent {
			return nil, errAt(pt, "函数的参数应该是名字")
		}
		par := paramX{t: pt}
		if p.isOp("=") {
			p.next()
			d, err := p.expr()
			if err != nil {
				return nil, err
			}
			par.def = d
		}
		f.params = append(f.params, par)
		if p.isOp(",") {
			p.next()
		} else if !p.isOp(")") {
			return nil, errAt(p.peek(), "参数之间要用逗号隔开")
		}
	}
	p.next() // )
	arrow := p.next()
	if p.atEnd() {
		body, err := p.indented(level, arrow)
		if err != nil {
			return nil, err
		}
		f.body = body
		return f, nil
	}
	x, err := p.valueExpr(level)
	if err != nil {
		return nil, err
	}
	f.body = []stmt{&exprS{x}}
	return f, nil
}

// isTupleDecl reports whether the line is [a, b] = …
func (p *parser) isTupleDecl() bool {
	for k := p.ti; k < len(p.toks); k++ {
		if t := p.toks[k]; t.kind == tkOp && t.text == "]" {
			return k+1 < len(p.toks) && p.toks[k+1].kind == tkOp && (p.toks[k+1].text == "=" || p.toks[k+1].text == ":=")
		}
	}
	return false
}

func (p *parser) tupleDecl(level int) (stmt, error) {
	t := p.next()
	d := &tupleDeclS{t: t}
	for !p.isOp("]") {
		n := p.next()
		if n.kind != tkIdent {
			return nil, errAt(n, "[ ] 里应该是变量名")
		}
		d.names = append(d.names, n)
		if p.isOp(",") {
			p.next()
		}
	}
	p.next()
	if p.isOp(":=") {
		return nil, errAt(p.peek(), "Candle 不支持用 := 给 [a, b] 赋值，请用 =")
	}
	p.next()
	x, err := p.valueExpr(level)
	if err != nil {
		return nil, err
	}
	d.x = x
	return d, nil
}

// The expressions, by Pine's precedence: ?: below or, and, == !=, < > <= >=,
// + -, * / %, then the unary operators and not, then [] and calls.

func (p *parser) expr() (expr, error) {
	if p.isWord("if") || p.isWord("switch") {
		return nil, errAt(p.peek(), "if、switch 当值用时要放在 = 后面，单独占一行")
	}
	c, err := p.binary(0)
	if err != nil {
		return nil, err
	}
	if !p.isOp("?") {
		return c, nil
	}
	t := p.next()
	a, err := p.expr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(":"); err != nil {
		return nil, err
	}
	b, err := p.expr()
	if err != nil {
		return nil, err
	}
	return &ternaryX{t: t, c: c, a: a, b: b}, nil
}

var levels = [][]string{{"or"}, {"and"}, {"==", "!="}, {"<", ">", "<=", ">="}, {"+", "-"}, {"*", "/", "%"}}

func (p *parser) binary(lv int) (expr, error) {
	if lv == len(levels) {
		return p.unary()
	}
	l, err := p.binary(lv + 1)
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if p.atEnd() || (t.kind != tkOp && t.kind != tkIdent) {
			return l, nil
		}
		found := false
		for _, op := range levels[lv] {
			if t.text == op {
				found = true
			}
		}
		if !found {
			return l, nil
		}
		p.next()
		r, err := p.binary(lv + 1)
		if err != nil {
			return nil, err
		}
		l = &binaryX{t: t, op: t.text, l: l, r: r}
	}
}

func (p *parser) unary() (expr, error) {
	if p.isOp("-") || p.isOp("+") || p.isWord("not") || p.isOp("!") {
		t := p.next()
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		op := t.text
		if op == "!" {
			op = "not"
		}
		return &unaryX{t: t, op: op, x: x}, nil
	}
	x, err := p.primary()
	if err != nil {
		return nil, err
	}
	for p.isOp("[") {
		t := p.next()
		off, err := p.expr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect("]"); err != nil {
			return nil, err
		}
		x = &indexX{t: t, x: x, off: off}
	}
	return x, nil
}

func (p *parser) primary() (expr, error) {
	t := p.next()
	switch t.kind {
	case tkNum:
		return &numLit{t}, nil
	case tkStr:
		return &strLit{t}, nil
	case tkColor:
		return &colorLit{t}, nil
	case tkIdent:
		switch t.text {
		case "true", "false":
			return &boolLit{t, t.text == "true"}, nil
		case "na":
			if !p.isOp("(") {
				return &naLit{t}, nil
			}
		case "and", "or", "not", "if", "else", "for", "to", "by", "while", "switch", "var", "varip":
			return nil, errAt(t, "这里不该出现 %s", t.text)
		}
		if strings.HasPrefix(t.text, "array.") || strings.HasPrefix(t.text, "matrix.") || strings.HasPrefix(t.text, "map.") {
			return nil, errAt(t, "Candle 不支持 %s（数组、矩阵、map）", t.text)
		}
		if !p.isOp("(") {
			return &nameRef{t}, nil
		}
		p.next()
		c := &callX{t: t, fn: t.text}
		for !p.isOp(")") {
			if p.atEnd() {
				return nil, errAt(p.peek(), "少了 )")
			}
			var a argX
			if p.peek().kind == tkIdent && p.peekAt(1).kind == tkOp && p.peekAt(1).text == "=" {
				nt := p.next()
				a.name, a.t = nt.text, nt
				p.next()
			}
			x, err := p.expr()
			if err != nil {
				return nil, err
			}
			if a.t.end == 0 {
				a.t = x.at()
			}
			a.x = x
			c.args = append(c.args, a)
			if p.isOp(",") {
				p.next()
			} else if p.atEnd() {
				return nil, errAt(p.peek(), "少了 )")
			} else if !p.isOp(")") {
				return nil, errAt(p.peek(), "参数之间要用逗号隔开")
			}
		}
		p.next()
		return c, nil
	case tkOp:
		switch t.text {
		case "(":
			x, err := p.expr()
			if err != nil {
				return nil, err
			}
			_, err = p.expect(")")
			return x, err
		case "[":
			tu := &tupleX{t: t}
			for !p.isOp("]") {
				if p.atEnd() {
					return nil, errAt(p.peek(), "少了 ]")
				}
				x, err := p.expr()
				if err != nil {
					return nil, err
				}
				tu.xs = append(tu.xs, x)
				if p.isOp(",") {
					p.next()
				}
			}
			p.next()
			return tu, nil
		}
	}
	if t.text == "" {
		return nil, errAt(t, "这一行还没写完")
	}
	return nil, errAt(t, "这里不该出现 %s", describe(t))
}
