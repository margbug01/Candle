package pine

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// What a compiled expression gives.
type vkind uint8

const (
	kFloat   vkind = iota // numbers, bools (1, 0), colors (packed RGBA) and ids
	kStr                  // strings
	kTuple                // [a, b, …] of floats
	kVoid                 // what plot's siblings that give nothing give
	kPlot                 // a plot, for fill
	kHline                // a level, for fill
	kMissing              // an argument not given
)

// cx is a compiled expression.
type cx struct {
	k    vkind
	f    func(m *Machine) float64
	s    func(m *Machine) string
	tu   func(m *Machine) []float64
	n    int // a tuple's size
	tok  token
	hist func(m *Machine, k int) float64 // the value k bars back, for x[k], when it has one of its own

	konst  bool // known as it compiles: kv or ks
	kv     float64
	ks     string
	simple bool   // the same on every bar: built of constants and inputs
	source string // which bar value a series is, for input.source
	id     int    // a plot's or level's index
}

func numX(f func(m *Machine) float64) cx { return cx{k: kFloat, f: f} }

func konstX(v float64) cx {
	return cx{k: kFloat, f: func(*Machine) float64 { return v }, konst: true, kv: v, simple: true}
}

func strX(v string) cx {
	return cx{k: kStr, s: func(*Machine) string { return v }, konst: true, ks: v, simple: true}
}

var naX = konstX(math.NaN())

type variable struct {
	k      vkind
	slot   int // a float buffer's, or a string's
	simple bool
	tok    token
}

type scope struct {
	parent *scope
	vars   map[string]*variable
}

func (s *scope) find(name string) *variable {
	for ; s != nil; s = s.parent {
		if v, ok := s.vars[name]; ok {
			return v
		}
	}
	return nil
}

type compiler struct {
	s       *Script
	funcs   map[string]*funcS
	global  *scope
	depth   int  // functions being inlined
	local   int  // blocks of if, for, switch and functions the code is in
	loops   int  // loops the code is in
	lazy    bool // and and or skip their right side when the left decides, as in version 6
	hint    string
	version int
}

var versionRE = regexp.MustCompile(`//\s*@version\s*=\s*(\d+)`)

// Compile reads a script.
func Compile(src string) (*Script, error) {
	body, err := parse(src)
	if err != nil {
		return nil, err
	}
	s := &Script{Precision: -1, Format: "inherit", src: src}
	if v := versionRE.FindStringSubmatch(src); v != nil {
		s.Version, _ = strconv.Atoi(v[1])
	}
	c := &compiler{s: s, funcs: map[string]*funcS{}, global: &scope{vars: map[string]*variable{}}, lazy: s.Version >= 6, version: s.Version}
	for name, f := range preludeFuncs() {
		c.funcs[name] = f
	}
	for _, st := range body { // functions can be called before they are defined
		if f, ok := st.(*funcS); ok {
			c.funcs[f.name] = f
		}
	}
	run, err := c.block(body, c.global)
	if err != nil {
		return nil, err
	}
	if !s.declared {
		return nil, &Error{Start: 0, End: min(len(src), 1), Msg: "缺少 indicator(\"名字\") 这一行"}
	}
	s.run = run
	return s, nil
}

func (c *compiler) newBuf(carry bool) int {
	c.s.carry = append(c.s.carry, carry)
	return len(c.s.carry) - 1
}

func (c *compiler) newStr() int {
	c.s.nstr++
	return c.s.nstr - 1
}

// block compiles statements as statements.
func (c *compiler) block(body []stmt, sc *scope) ([]func(*Machine), error) {
	var out []func(*Machine)
	for _, st := range body {
		f, err := c.stmt(st, sc)
		if err != nil {
			return nil, err
		}
		if f != nil {
			out = append(out, f)
		}
	}
	return out, nil
}

func runBlock(m *Machine, ss []func(*Machine)) {
	for _, s := range ss {
		s(m)
		if m.jump != 0 {
			return
		}
	}
}

// blockValue compiles a block whose last statement, an expression, is its
// value; without one it is na.
func (c *compiler) blockValue(body []stmt, sc *scope) ([]func(*Machine), cx, error) {
	if len(body) == 0 {
		return nil, naX, nil
	}
	last, ok := body[len(body)-1].(*exprS)
	if !ok {
		ss, err := c.block(body, sc)
		return ss, naX, err
	}
	ss, err := c.block(body[:len(body)-1], sc)
	if err != nil {
		return nil, cx{}, err
	}
	v, err := c.value(last.x, sc)
	return ss, v, err
}

// value compiles what a block ends with: an expression, or a tuple.
func (c *compiler) value(x expr, sc *scope) (cx, error) {
	if tu, ok := x.(*tupleX); ok {
		parts := make([]cx, len(tu.xs))
		for i, e := range tu.xs {
			v, err := c.expr(e, sc)
			if err != nil {
				return cx{}, err
			}
			if v.k != kFloat {
				return cx{}, errAt(e.at(), "[ ] 里只能放数值")
			}
			parts[i] = v
		}
		return cx{k: kTuple, n: len(parts), tok: tu.t, tu: func(m *Machine) []float64 {
			out := make([]float64, len(parts))
			for i, p := range parts {
				out[i] = p.f(m)
			}
			return out
		}}, nil
	}
	return c.expr(x, sc)
}

func truth(v float64) bool { return v != 0 && !math.IsNaN(v) }

func boolF(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func (c *compiler) stmt(st stmt, sc *scope) (func(*Machine), error) {
	switch st := st.(type) {
	case *funcS:
		if c.local > 0 || c.depth > 0 {
			return nil, errAt(st.t, "函数只能在最外层定义")
		}
		return nil, nil
	case *exprS:
		v, err := c.expr(st.x, sc)
		if err != nil {
			return nil, err
		}
		switch v.k {
		case kFloat, kPlot, kHline, kVoid:
			if v.f == nil {
				return nil, nil
			}
			return func(m *Machine) { v.f(m) }, nil
		case kStr:
			return func(m *Machine) { v.s(m) }, nil
		case kTuple:
			return func(m *Machine) { v.tu(m) }, nil
		}
		return nil, nil
	case *declS:
		return c.decl(st, sc)
	case *tupleDeclS:
		v, err := c.value(st.x, sc)
		if err != nil {
			return nil, err
		}
		if v.k != kTuple {
			return nil, errAt(st.t, "右边不是 [a, b] 这样的多个值")
		}
		if v.n != len(st.names) {
			return nil, errAt(st.t, "右边有 %d 个值，左边是 %d 个变量", v.n, len(st.names))
		}
		slots := make([]int, len(st.names))
		for i, n := range st.names {
			if n.text == "_" {
				slots[i] = -1
				continue
			}
			if _, ok := sc.vars[n.text]; ok {
				return nil, errAt(n, "%s 已经声明过了，要改它的值请用 :=", n.text)
			}
			slots[i] = c.newBuf(false)
			sc.vars[n.text] = &variable{k: kFloat, slot: slots[i], tok: n}
		}
		return func(m *Machine) {
			vals := v.tu(m)
			for i, s := range slots {
				if s >= 0 {
					m.buf[s][m.i] = vals[i]
				}
			}
		}, nil
	case *assignS:
		return c.assign(st, sc)
	case *forS:
		return c.forStmt(st, sc)
	case *whileS:
		cond, err := c.expr(st.cond, sc)
		if err != nil {
			return nil, err
		}
		if cond.k != kFloat {
			return nil, errAt(st.t, "while 的条件要是真假值")
		}
		c.local++
		c.loops++
		body, err := c.block(st.body, &scope{parent: sc, vars: map[string]*variable{}})
		c.local--
		c.loops--
		if err != nil {
			return nil, err
		}
		return func(m *Machine) {
			for n := 0; truth(cond.f(m)); n++ {
				m.loop(n)
				runBlock(m, body)
				if m.jump == jumpBreak {
					m.jump = 0
					break
				}
				m.jump = 0
			}
		}, nil
	case *breakS, *continueS:
		var t token
		j := jumpBreak
		if b, ok := st.(*breakS); ok {
			t = b.t
		} else {
			t, j = st.(*continueS).t, jumpContinue
		}
		if c.loops == 0 {
			return nil, errAt(t, "%s 只能用在循环里", t.text)
		}
		return func(m *Machine) { m.jump = j }, nil
	}
	return nil, nil
}

const (
	jumpBreak = 1 + iota
	jumpContinue
)

func (c *compiler) decl(st *declS, sc *scope) (func(*Machine), error) {
	if _, ok := sc.vars[st.name]; ok {
		return nil, errAt(st.t, "%s 已经声明过了，要改它的值请用 :=", st.name)
	}
	c.hint = st.name
	v, err := c.value(st.x, sc)
	c.hint = ""
	if err != nil {
		return nil, err
	}
	if st.typ == "string" && v.k == kFloat && v.konst && math.IsNaN(v.kv) {
		v = strX("")
	}
	switch v.k {
	case kTuple:
		return nil, errAt(st.t, "右边有好几个值，要写成 [a, b] = …")
	case kVoid, kMissing:
		return nil, errAt(st.t, "右边没有值")
	case kPlot, kHline: // p = plot(…), for fill: the name stands for the plot
		sc.vars[st.name] = &variable{k: v.k, slot: v.id, tok: st.t}
		return func(m *Machine) { v.f(m) }, nil
	case kStr:
		slot := c.newStr()
		sc.vars[st.name] = &variable{k: kStr, slot: slot, simple: v.simple && st.mode == "", tok: st.t}
		if st.mode != "" {
			init := c.newBuf(true)
			return func(m *Machine) {
				if m.buf[init][m.i] != 1 {
					m.buf[init][m.i] = 1
					m.str[slot] = v.s(m)
				}
			}, nil
		}
		return func(m *Machine) { m.str[slot] = v.s(m) }, nil
	}
	slot := c.newBuf(st.mode != "")
	sc.vars[st.name] = &variable{k: kFloat, slot: slot, simple: v.simple && st.mode == "", tok: st.t}
	if st.mode != "" {
		init := c.newBuf(true)
		return func(m *Machine) {
			if m.buf[init][m.i] != 1 {
				m.buf[init][m.i] = 1
				m.buf[slot][m.i] = v.f(m)
			}
		}, nil
	}
	return func(m *Machine) { m.buf[slot][m.i] = v.f(m) }, nil
}

func (c *compiler) assign(st *assignS, sc *scope) (func(*Machine), error) {
	vr := sc.find(st.name)
	if vr == nil {
		if isBuiltinName(st.name) {
			return nil, errAt(st.t, "%s 是内置的，不能改", st.name)
		}
		return nil, errAt(st.t, "%s 还没声明，第一次给值要用 =", st.name)
	}
	if vr.k == kPlot || vr.k == kHline {
		return nil, errAt(st.t, "%s 是 plot/hline，不能改", st.name)
	}
	v, err := c.value(st.x, sc)
	if err != nil {
		return nil, err
	}
	vr.simple = false
	slot := vr.slot
	if vr.k == kStr {
		if v.k != kStr {
			if v.k == kFloat && v.konst && math.IsNaN(v.kv) {
				v = strX("")
			} else {
				return nil, errAt(st.t, "%s 是文字，不能换成数值", st.name)
			}
		}
		switch st.op {
		case ":=":
			return func(m *Machine) { m.str[slot] = v.s(m) }, nil
		case "+=":
			return func(m *Machine) { m.str[slot] += v.s(m) }, nil
		}
		return nil, errAt(st.t, "文字只能用 := 和 +=")
	}
	if v.k != kFloat {
		return nil, errAt(st.t, "%s 是数值，右边也要是数值", st.name)
	}
	op := arith(st.op[:1])
	if st.op == ":=" {
		return func(m *Machine) { m.buf[slot][m.i] = v.f(m) }, nil
	}
	return func(m *Machine) { m.buf[slot][m.i] = op(m.buf[slot][m.i], v.f(m)) }, nil
}

func arith(op string) func(a, b float64) float64 {
	switch op {
	case "+":
		return func(a, b float64) float64 { return a + b }
	case "-":
		return func(a, b float64) float64 { return a - b }
	case "*":
		return func(a, b float64) float64 { return a * b }
	case "/":
		return func(a, b float64) float64 {
			if b == 0 {
				return math.NaN()
			}
			return a / b
		}
	case "%":
		return func(a, b float64) float64 {
			if b == 0 {
				return math.NaN()
			}
			return math.Mod(a, b)
		}
	}
	return nil
}

func (c *compiler) forStmt(st *forS, sc *scope) (func(*Machine), error) {
	from, err := c.expr(st.from, sc)
	if err != nil {
		return nil, err
	}
	to, err := c.expr(st.to, sc)
	if err != nil {
		return nil, err
	}
	by := cx{}
	if st.by != nil {
		if by, err = c.expr(st.by, sc); err != nil {
			return nil, err
		}
	}
	if from.k != kFloat || to.k != kFloat || st.by != nil && by.k != kFloat {
		return nil, errAt(st.t, "for 的起止要是数")
	}
	inner := &scope{parent: sc, vars: map[string]*variable{}}
	slot := c.newBuf(false)
	inner.vars[st.v] = &variable{k: kFloat, slot: slot, tok: st.t}
	c.local++
	c.loops++
	body, err := c.block(st.body, inner)
	c.local--
	c.loops--
	if err != nil {
		return nil, err
	}
	return func(m *Machine) {
		a, b := from.f(m), to.f(m)
		if math.IsNaN(a) || math.IsNaN(b) {
			return
		}
		step := 1.0
		if by.f != nil {
			step = math.Abs(by.f(m))
		}
		if step == 0 || math.IsNaN(step) {
			return
		}
		if b < a {
			step = -step
		}
		n := 0
		for v := a; step > 0 && v <= b || step < 0 && v >= b; v += step {
			m.loop(n)
			n++
			m.buf[slot][m.i] = v
			runBlock(m, body)
			if m.jump == jumpBreak {
				m.jump = 0
				break
			}
			m.jump = 0
		}
	}, nil
}

// expr compiles an expression that gives one value.
func (c *compiler) expr(x expr, sc *scope) (cx, error) {
	v, err := c.expr1(x, sc)
	if err != nil {
		return v, err
	}
	if v.tok.end == 0 {
		v.tok = x.at()
	}
	return v, nil
}

func (c *compiler) expr1(x expr, sc *scope) (cx, error) {
	switch x := x.(type) {
	case *numLit:
		return konstX(x.t.num), nil
	case *strLit:
		return strX(x.t.text), nil
	case *colorLit:
		return konstX(parseColor(x.t.text)), nil
	case *boolLit:
		return konstX(boolF(x.v)), nil
	case *naLit:
		return naX, nil
	case *nameRef:
		return c.name(x.t, sc)
	case *callX:
		return c.call(x, sc)
	case *indexX:
		return c.index(x, sc)
	case *unaryX:
		v, err := c.expr(x.x, sc)
		if err != nil {
			return cx{}, err
		}
		if v.k != kFloat {
			return cx{}, errAt(x.t, "%s 只能用在数值上", x.op)
		}
		var out cx
		switch x.op {
		case "-":
			out = numX(func(m *Machine) float64 { return -v.f(m) })
		case "+":
			out = v
		default:
			out = numX(func(m *Machine) float64 {
				a := v.f(m)
				if math.IsNaN(a) {
					return math.NaN()
				}
				return boolF(!truth(a))
			})
		}
		out.simple = v.simple
		if v.konst {
			return konstX(out.f(nil)), nil
		}
		return out, nil
	case *binaryX:
		return c.binary(x, sc)
	case *ternaryX:
		cond, err := c.expr(x.c, sc)
		if err != nil {
			return cx{}, err
		}
		a, err := c.expr(x.a, sc)
		if err != nil {
			return cx{}, err
		}
		b, err := c.expr(x.b, sc)
		if err != nil {
			return cx{}, err
		}
		a, b, err = unify(a, b, x.t)
		if err != nil {
			return cx{}, err
		}
		simple := cond.simple && a.simple && b.simple
		if a.k == kStr {
			return cx{k: kStr, simple: simple, s: func(m *Machine) string {
				if truth(cond.f(m)) {
					return a.s(m)
				}
				return b.s(m)
			}}, nil
		}
		return cx{k: kFloat, simple: simple, f: func(m *Machine) float64 {
			if truth(cond.f(m)) {
				return a.f(m)
			}
			return b.f(m)
		}}, nil
	case *tupleX:
		return cx{}, errAt(x.t, "[a, b] 只能写在函数的最后一行，当返回值")
	case *ifX:
		return c.ifExpr(x, sc)
	case *switchX:
		return c.switchExpr(x, sc)
	}
	return cx{}, errAt(x.at(), "看不懂这里")
}

// unify makes a and b the same kind, na taking the other's.
func unify(a, b cx, t token) (cx, cx, error) {
	isNa := func(v cx) bool { return v.k == kFloat && v.konst && math.IsNaN(v.kv) }
	switch {
	case a.k == b.k:
	case a.k == kStr && isNa(b):
		b = strX("")
	case b.k == kStr && isNa(a):
		a = strX("")
	default:
		return a, b, errAt(t, "两边的值不是一类（一边是文字，一边是数值）")
	}
	if a.k != kFloat && a.k != kStr {
		return a, b, errAt(t, "这里只能是数值或文字")
	}
	return a, b, nil
}

func (c *compiler) binary(x *binaryX, sc *scope) (cx, error) {
	l, err := c.expr(x.l, sc)
	if err != nil {
		return cx{}, err
	}
	r, err := c.expr(x.r, sc)
	if err != nil {
		return cx{}, err
	}
	simple := l.simple && r.simple
	if l.k == kStr || r.k == kStr {
		if l.k != r.k {
			return cx{}, errAt(x.t, "文字和数值不能直接 %s，数值请先用 str.tostring() 转成文字", x.op)
		}
		var out cx
		switch x.op {
		case "+":
			out = cx{k: kStr, s: func(m *Machine) string { return l.s(m) + r.s(m) }}
		case "==":
			out = numX(func(m *Machine) float64 { return boolF(l.s(m) == r.s(m)) })
		case "!=":
			out = numX(func(m *Machine) float64 { return boolF(l.s(m) != r.s(m)) })
		default:
			return cx{}, errAt(x.t, "文字不能用 %s", x.op)
		}
		out.simple = simple
		if l.konst && r.konst {
			if out.k == kStr {
				return strX(out.s(nil)), nil
			}
			return konstX(out.f(nil)), nil
		}
		return out, nil
	}
	if l.k != kFloat || r.k != kFloat {
		return cx{}, errAt(x.t, "%s 两边要是数值", x.op)
	}
	var out cx
	switch x.op {
	case "+", "-", "*", "/", "%":
		op := arith(x.op)
		out = numX(func(m *Machine) float64 { return op(l.f(m), r.f(m)) })
	case "==", "!=", "<", ">", "<=", ">=":
		var cmp func(a, b float64) bool
		switch x.op {
		case "==":
			cmp = func(a, b float64) bool { return a == b }
		case "!=":
			cmp = func(a, b float64) bool { return a != b }
		case "<":
			cmp = func(a, b float64) bool { return a < b }
		case ">":
			cmp = func(a, b float64) bool { return a > b }
		case "<=":
			cmp = func(a, b float64) bool { return a <= b }
		default:
			cmp = func(a, b float64) bool { return a >= b }
		}
		out = numX(func(m *Machine) float64 {
			a, b := l.f(m), r.f(m)
			if math.IsNaN(a) || math.IsNaN(b) {
				if x.op == "!=" && math.IsNaN(a) != math.IsNaN(b) {
					return 1
				}
				return boolF(x.op == "==" && math.IsNaN(a) && math.IsNaN(b))
			}
			return boolF(cmp(a, b))
		})
	case "and":
		if c.lazy {
			out = numX(func(m *Machine) float64 { return boolF(truth(l.f(m)) && truth(r.f(m))) })
		} else {
			out = numX(func(m *Machine) float64 { a, b := truth(l.f(m)), truth(r.f(m)); return boolF(a && b) })
		}
	case "or":
		if c.lazy {
			out = numX(func(m *Machine) float64 { return boolF(truth(l.f(m)) || truth(r.f(m))) })
		} else {
			out = numX(func(m *Machine) float64 { a, b := truth(l.f(m)), truth(r.f(m)); return boolF(a || b) })
		}
	default:
		return cx{}, errAt(x.t, "不认识运算符 %s", x.op)
	}
	out.simple = simple
	if l.konst && r.konst {
		return konstX(out.f(nil)), nil
	}
	return out, nil
}

func (c *compiler) ifExpr(x *ifX, sc *scope) (cx, error) {
	conds := make([]cx, len(x.conds))
	blocks := make([][]func(*Machine), len(x.conds))
	vals := make([]cx, len(x.conds))
	c.local++
	defer func() { c.local-- }()
	for i, ce := range x.conds {
		cond, err := c.expr(ce, sc)
		if err != nil {
			return cx{}, err
		}
		if cond.k != kFloat {
			return cx{}, errAt(ce.at(), "if 的条件要是真假值")
		}
		conds[i] = cond
		if blocks[i], vals[i], err = c.blockValue(x.blocks[i], &scope{parent: sc, vars: map[string]*variable{}}); err != nil {
			return cx{}, err
		}
	}
	var els []func(*Machine)
	ev := naX
	if x.els != nil {
		var err error
		if els, ev, err = c.blockValue(x.els, &scope{parent: sc, vars: map[string]*variable{}}); err != nil {
			return cx{}, err
		}
	}
	return branches(x.t, conds, blocks, vals, els, ev, func(m *Machine, i int) bool { return truth(conds[i].f(m)) })
}

// branches is the value of the first block whose test passes, else els's.
func branches(t token, conds []cx, blocks [][]func(*Machine), vals []cx, els []func(*Machine), ev cx, test func(m *Machine, i int) bool) (cx, error) {
	k := ev.k
	for _, v := range vals {
		if v.k == kStr {
			k = kStr
		} else if v.k == kTuple {
			k = kTuple
		}
	}
	all := append(append([]cx(nil), vals...), ev)
	for i, v := range all {
		switch {
		case v.k == k:
		case k == kStr && v.k == kFloat && v.konst && math.IsNaN(v.kv):
			all[i] = strX("")
		case k == kTuple || v.k != kFloat || k != kFloat:
			if k == kTuple && v.k == kFloat && v.konst && math.IsNaN(v.kv) {
				continue
			}
			if !(v.k == kVoid || v.k == kPlot || v.k == kHline) {
				return cx{}, errAt(t, "各个分支的值不是一类")
			}
			all[i] = naX
		}
	}
	vals, ev = all[:len(vals)], all[len(vals)]
	pick := func(m *Machine) int {
		for i := range conds {
			if test(m, i) {
				runBlock(m, blocks[i])
				return i
			}
		}
		runBlock(m, els)
		return -1
	}
	at := func(i int) cx {
		if i < 0 {
			return ev
		}
		return vals[i]
	}
	switch k {
	case kStr:
		return cx{k: kStr, s: func(m *Machine) string { return at(pick(m)).s(m) }}, nil
	case kTuple:
		n := 0
		for _, v := range all {
			if v.k == kTuple {
				n = v.n
			}
		}
		return cx{k: kTuple, n: n, tu: func(m *Machine) []float64 {
			v := at(pick(m))
			if v.k != kTuple {
				out := make([]float64, n)
				for i := range out {
					out[i] = math.NaN()
				}
				return out
			}
			return v.tu(m)
		}}, nil
	}
	return numX(func(m *Machine) float64 {
		v := at(pick(m))
		if v.f == nil {
			return math.NaN()
		}
		return v.f(m)
	}), nil
}

func (c *compiler) switchExpr(x *switchX, sc *scope) (cx, error) {
	var subject cx
	if x.subject != nil {
		var err error
		if subject, err = c.expr(x.subject, sc); err != nil {
			return cx{}, err
		}
	}
	c.local++
	defer func() { c.local-- }()
	var conds []cx
	var blocks [][]func(*Machine)
	var vals []cx
	var els []func(*Machine)
	ev := naX
	for _, cs := range x.cases {
		ss, v, err := c.blockValue(cs.body, &scope{parent: sc, vars: map[string]*variable{}})
		if err != nil {
			return cx{}, err
		}
		if cs.match == nil {
			els, ev = ss, v
			continue
		}
		mt, err := c.expr(cs.match, sc)
		if err != nil {
			return cx{}, err
		}
		if x.subject != nil && mt.k != subject.k {
			return cx{}, errAt(cs.match.at(), "这个分支和 switch 的值不是一类")
		}
		conds, blocks, vals = append(conds, mt), append(blocks, ss), append(vals, v)
	}
	test := func(m *Machine, i int) bool { return truth(conds[i].f(m)) }
	if x.subject != nil {
		if subject.k == kStr {
			test = func(m *Machine, i int) bool { return subject.s(m) == conds[i].s(m) }
		} else {
			test = func(m *Machine, i int) bool { return subject.f(m) == conds[i].f(m) }
		}
	}
	return branches(x.t, conds, blocks, vals, els, ev, test)
}

// index compiles x[k], x's value k bars back.
func (c *compiler) index(x *indexX, sc *scope) (cx, error) {
	base, err := c.expr(x.x, sc)
	if err != nil {
		return cx{}, err
	}
	off, err := c.expr(x.off, sc)
	if err != nil {
		return cx{}, err
	}
	if off.k != kFloat {
		return cx{}, errAt(x.t, "[ ] 里要是数字")
	}
	if base.k != kFloat {
		return cx{}, errAt(x.t, "只有数值能取历史值 [ ]")
	}
	if off.konst && off.kv < 0 {
		return cx{}, errAt(x.t, "历史引用不能是负数")
	}
	hist := base.hist
	if hist == nil { // kept as it is worked out
		buf := c.newBuf(false)
		eval := base.f
		hist = func(m *Machine, k int) float64 {
			m.buf[buf][m.i] = eval(m)
			if k > m.i {
				return math.NaN()
			}
			return m.buf[buf][m.i-k]
		}
	}
	return numX(func(m *Machine) float64 {
		k := off.f(m)
		if math.IsNaN(k) || k < 0 {
			return math.NaN()
		}
		return hist(m, int(k))
	}), nil
}

// bufHist is the history of a buffer.
func bufHist(buf int) func(m *Machine, k int) float64 {
	return func(m *Machine, k int) float64 {
		if k > m.i {
			return math.NaN()
		}
		return m.buf[buf][m.i-k]
	}
}

func (c *compiler) name(t token, sc *scope) (cx, error) {
	if v := sc.find(t.text); v != nil {
		switch v.k {
		case kStr:
			slot := v.slot
			return cx{k: kStr, simple: v.simple, s: func(m *Machine) string { return m.str[slot] }}, nil
		case kPlot, kHline:
			return cx{k: v.k, id: v.slot}, nil
		}
		slot := v.slot
		return cx{k: kFloat, simple: v.simple, f: func(m *Machine) float64 { return m.buf[slot][m.i] }, hist: bufHist(slot)}, nil
	}
	if b, ok := builtinVars[t.text]; ok {
		return b(c), nil
	}
	if b, ok := builtinVars["ta."+t.text]; ok && c.version < 5 { // version 4's names
		return b(c), nil
	}
	if _, ok := c.funcs[t.text]; ok {
		return cx{}, errAt(t, "%s 是函数，要加括号调用", t.text)
	}
	if head, _, ok := strings.Cut(t.text, "."); ok {
		if v := sc.find(head); v != nil || skipped[head] { // a field of a label or a line, a constant for one: Candle draws none
			return naX, nil
		}
	}
	return cx{}, errAt(t, "不认识 %s", t.text)
}

func isBuiltinName(n string) bool {
	_, ok := builtinVars[n]
	return ok
}

// call compiles a call of one of the script's functions, or a built-in one.
func (c *compiler) call(x *callX, sc *scope) (cx, error) {
	if f, ok := c.funcs[x.fn]; ok {
		return c.inline(f, x, sc)
	}
	if head, _, ok := strings.Cut(x.fn, "."); ok && sc.find(head) != nil { // a method of a label, a line…
		return naX, nil
	}
	ns, _, _ := strings.Cut(x.fn, ".")
	switch ns {
	case "label", "line", "box", "table", "polyline", "linefill", "chart":
		c.warn(x.t, "Candle 不画 label、line、box、table，%s 会被跳过", x.fn)
		return naX, nil
	case "request":
		return cx{}, errAt(x.t, "Candle 拿不到别的品种或周期的数据（%s）", x.fn)
	case "strategy":
		return cx{}, errAt(x.t, "这是策略脚本里的 %s，Candle 只跑指标", x.fn)
	case "array", "matrix", "map":
		return cx{}, errAt(x.t, "Candle 不支持 %s（数组、矩阵、map）", x.fn)
	case "log", "runtime":
		return naX, nil
	}
	b, ok := builtinFuncs[x.fn]
	if !ok && c.version < 5 {
		for _, pre := range []string{"ta.", "math.", "input.", "str."} {
			if b, ok = builtinFuncs[pre+x.fn]; ok {
				break
			}
		}
		if alias, found := v4Inputs[x.fn]; found && !ok {
			b, ok = builtinFuncs[alias]
		}
	}
	if !ok {
		switch x.fn {
		case "strategy", "library":
			return cx{}, errAt(x.t, "这是%s脚本，Candle 只跑指标（indicator）", map[string]string{"strategy": "策略", "library": "库"}[x.fn])
		case "barcolor", "alertcondition", "alert", "max_bars_back", "plotcandle", "plotbar":
			if x.fn == "plotcandle" || x.fn == "plotbar" {
				c.warn(x.t, "Candle 还不画 %s，会被跳过", x.fn)
			}
			return cx{k: kVoid}, nil
		}
		return cx{}, errAt(x.t, "Candle 不认识函数 %s", x.fn)
	}
	args, err := c.args(x, b.params, sc)
	if err != nil {
		return cx{}, err
	}
	if b.global && (c.local > 0 || c.depth > 0) {
		return cx{}, errAt(x.t, "%s 只能写在最外层，不能放在 if、for 或函数里", x.fn)
	}
	v, err := b.compile(c, args, x)
	if err != nil {
		return cx{}, err
	}
	if b.pure {
		v.simple = true
		konst := true
		for _, a := range args {
			if a.k == kMissing {
				continue
			}
			v.simple = v.simple && a.simple
			konst = konst && a.konst
		}
		if konst && v.k == kFloat && v.f != nil {
			return konstX(v.f(nil)), nil
		}
	}
	return v, nil
}

// args lines up a call's arguments with params, by place, then by name.
func (c *compiler) args(x *callX, params []string, sc *scope) ([]cx, error) {
	out := make([]cx, len(params))
	for i := range out {
		out[i] = cx{k: kMissing}
	}
	named := false
	for i, a := range x.args {
		at := i
		if a.name != "" {
			named = true
			at = -1
			for j, p := range params {
				if p == a.name {
					at = j
				}
			}
			if at < 0 {
				if ignoredArgs[a.name] {
					continue
				}
				return nil, errAt(a.t, "%s 没有叫 %s 的参数", x.fn, a.name)
			}
		} else if named {
			return nil, errAt(a.t, "带名字的参数后面不能再跟不带名字的")
		}
		if at >= len(params) {
			if variadic(params) {
				params = append(params, params[len(params)-1])
				out = append(out, cx{k: kMissing})
			} else {
				return nil, errAt(a.t, "%s 最多 %d 个参数", x.fn, len(params))
			}
		}
		if _, ok := a.x.(*tupleX); ok && params[at] == "options" { // read as it is written, by the input
			out[at] = cx{k: kVoid, tok: a.x.at()}
			continue
		}
		h := c.hint
		if at > 0 {
			c.hint = ""
		}
		v, err := c.expr(a.x, sc)
		c.hint = h
		if err != nil {
			return nil, err
		}
		v.tok = a.x.at()
		out[at] = v
	}
	return out, nil
}

func variadic(params []string) bool {
	return len(params) > 0 && strings.HasSuffix(params[len(params)-1], "…")
}

// Arguments Candle has no use for, which any call may be given.
var ignoredArgs = map[string]bool{"editable": true, "tooltip": true, "inline": true, "group": true, "confirm": true,
	"display": true, "show_last": true, "trackprice": true, "join": true, "fillgaps": true, "force_overlay": true,
	"max_bars_back": true, "explicit_plot_zorder": true, "max_lines_count": true, "max_labels_count": true,
	"max_boxes_count": true, "max_polylines_count": true, "calc_bars_count": true, "dynamic_requests": true,
	"behind_chart": true, "timeframe": true, "timeframe_gaps": true, "scale": true, "transp": true, "active": true,
	"histbase": true, "textcolor": true, "size": true, "text": true, "offset": true}

// inline compiles a call of the script's own function f where it is
// called: what it keeps, as a moving average in it, is this call's own.
func (c *compiler) inline(f *funcS, x *callX, sc *scope) (cx, error) {
	if c.depth > 24 {
		return cx{}, errAt(x.t, "函数 %s 调用自己，Candle 不支持递归", f.name)
	}
	params := make([]string, len(f.params))
	for i, p := range f.params {
		params[i] = p.t.text
	}
	args, err := c.args(x, params, sc)
	if err != nil {
		return cx{}, err
	}
	inner := &scope{parent: c.global, vars: map[string]*variable{}}
	type bind struct {
		v    cx
		slot int
	}
	binds := make([]bind, len(args))
	for i, a := range args {
		if a.k == kMissing {
			if f.params[i].def == nil {
				return cx{}, errAt(x.t, "调用 %s 少了参数 %s", f.name, params[i])
			}
			if a, err = c.expr(f.params[i].def, c.global); err != nil {
				return cx{}, err
			}
		}
		switch a.k {
		case kStr:
			binds[i] = bind{a, c.newStr()}
			inner.vars[params[i]] = &variable{k: kStr, slot: binds[i].slot, simple: a.simple}
		case kFloat:
			binds[i] = bind{a, c.newBuf(false)}
			inner.vars[params[i]] = &variable{k: kFloat, slot: binds[i].slot, simple: a.simple}
		default:
			return cx{}, errAt(a.tok, "这个参数要是数值或文字")
		}
	}
	c.depth++
	c.local++
	ss, v, err := c.blockValue(f.body, inner)
	c.depth--
	c.local--
	if err != nil {
		return cx{}, err
	}
	setup := func(m *Machine) {
		for _, b := range binds {
			if b.v.k == kStr {
				m.str[b.slot] = b.v.s(m)
			} else {
				m.buf[b.slot][m.i] = b.v.f(m)
			}
		}
		runBlock(m, ss)
	}
	switch v.k {
	case kStr:
		return cx{k: kStr, s: func(m *Machine) string { setup(m); return v.s(m) }}, nil
	case kTuple:
		return cx{k: kTuple, n: v.n, tu: func(m *Machine) []float64 { setup(m); return v.tu(m) }}, nil
	case kFloat:
		return numX(func(m *Machine) float64 { setup(m); return v.f(m) }), nil
	}
	return cx{k: kVoid, f: func(m *Machine) float64 { setup(m); return math.NaN() }}, nil
}

func (c *compiler) warn(t token, format string, args ...any) {
	e := errAt(t, format, args...)
	for _, w := range c.s.Warnings {
		if w.Msg == e.Msg {
			return
		}
	}
	c.s.Warnings = append(c.s.Warnings, e)
}

// parseColor reads #RRGGBB or #RRGGBBAA as a packed color.
func parseColor(s string) float64 {
	v, _ := strconv.ParseUint(s[1:], 16, 32)
	if len(s) == 7 {
		v = v<<8 | 0xff
	}
	return float64(uint32(v))
}
