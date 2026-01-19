// Package ddquery provides a parser for Datadog query expressions.
//
// The package parses Datadog query syntax into an abstract syntax tree (AST)
// that can be programmatically inspected and manipulated. It supports:
//
//   - Metric queries with aggregators (avg, sum, max, etc.)
//   - Tag scopes with boolean operators (AND, OR, NOT)
//   - Group-by clauses
//   - Function calls and nested functions
//   - Modifiers (fill, rollup, as_count, etc.)
//   - Variables ($var)
//   - IN clauses for tag filtering
//
// Example:
//
//	query, err := Parse("avg:metric.name{env:prod,service:api} by {host}.as_count()")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	mq, ok := query.(*MetricQuery)
//	if ok {
//	    fmt.Printf("Aggregator: %s\n", *mq.Aggregator)
//	    fmt.Printf("Metric: %s\n", mq.Metric)
//	}
//
// The parser returns detailed error messages with source code context when
// parsing fails, making it easy to identify and fix syntax errors.
package ddquery

import (
	"fmt"
	"strings"
)

// ParseError represents a parsing error with position information and context.
//
// The error includes the byte position where the error occurred, a descriptive
// message, and the source string for generating context-aware error messages.
type ParseError struct {
	Pos int    // Byte position in the source string where the error occurred
	Msg string // Descriptive error message
	Src string // Source string for context (used in Error() method)
}

// Error returns a formatted error message with source code context.
//
// The error message includes:
//   - The byte position where the error occurred
//   - A descriptive error message
//   - A snippet of the source code around the error position
//   - A visual indicator (^) showing the exact error location
//
// Example output:
//
//	parse error at position 21: unexpected token "extra" after end of expression
//	  vg:metric{env:prod} extra
//	                      ^
func (e *ParseError) Error() string {
	msg := fmt.Sprintf("parse error at position %d: %s", e.Pos, e.Msg)
	if e.Src != "" && e.Pos >= 0 && e.Pos < len(e.Src) {
		// Add context: show the position in the source
		start := e.Pos
		if start > 20 {
			start = e.Pos - 20
		}
		end := e.Pos + 20
		if end > len(e.Src) {
			end = len(e.Src)
		}
		context := e.Src[start:end]
		offset := e.Pos - start
		msg += fmt.Sprintf("\n  %s\n  %s^", context, strings.Repeat(" ", offset))
	}
	return msg
}

type Parser struct {
	l   *lexer
	src string
}

// Parse parses a Datadog query string and returns the corresponding AST.
//
// The function accepts a Datadog query expression string and returns either:
//   - An Expr representing the parsed query (MetricQuery, FuncCall, or literal)
//   - An error if the input is invalid
//
// Supported query formats:
//
//   - Metric queries: "avg:metric.name{env:prod} by {host}.as_count()"
//   - Function calls: "per_hour(avg:metric.name{env:prod})"
//   - Nested functions: "outliers(per_hour(avg:metric.name{env:prod}), 'DBSCAN', 3)"
//   - Boolean scopes: "metric{(env:prd OR env:shd) AND $product}"
//   - IN clauses: "metric{key IN (val1, val2, val3)}"
//
// Example:
//
//	query, err := Parse("sum:kubernetes.pods.running{env:prod} by {namespace}")
//	if err != nil {
//	    return fmt.Errorf("parse error: %w", err)
//	}
//
//	mq, ok := query.(*MetricQuery)
//	if !ok {
//	    return fmt.Errorf("expected metric query")
//	}
//
//	fmt.Printf("Parsed: %s:%s\n", *mq.Aggregator, mq.Metric)
//
// Returns an error of type *ParseError if parsing fails, which includes
// detailed position information and source code context.
func Parse(s string) (Expr, error) {
	p := &Parser{l: newLexer(s), src: s}
	ex, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.l.peek().typ != tEOF {
		t := p.l.peek()
		return nil, p.errf(t, "unexpected token %q after end of expression", t.lit)
	}
	return ex, nil
}

// Expr grammar (minimal): Primary only (Datadog arithmetic exists but not needed for your examples).
func (p *Parser) parseExpr() (Expr, error) {
	return p.parsePrimary()
}

func (p *Parser) parsePrimary() (Expr, error) {
	t := p.l.peek()

	// parenthesized expression (mostly for nested query args)
	if t.typ == tLParen {
		p.l.advance()
		ex, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(tRParen, "expected ')' to close parenthesized expression"); err != nil {
			return nil, err
		}
		return ex, nil
	}

	// literals
	if t.typ == tString {
		// Check for unterminated string (lexer returns string token even if unterminated)
		// We can detect this by checking if the string ends at EOF without proper closing
		// For now, we accept the string as-is since the lexer handles it
		p.l.advance()
		return &StringLit{Value: t.lit}, nil
	}
	if t.typ == tNumber {
		p.l.advance()
		f, err := parseFloatStrict(t.lit)
		if err != nil {
			return nil, &ParseError{Pos: t.pos, Msg: err.Error(), Src: p.src}
		}
		return &NumberLit{Value: f}, nil
	}

	// IDENT: could be funcCall, metricQuery, or ident literal (e.g. "zero" in fill(zero))
	if t.typ == tIdent {
		// function call lookahead: IDENT '('
		if p.peek2().typ == tLParen {
			return p.parseFuncCall()
		}

		// metric query or ident literal:
		// If pattern is IDENT ':' => metric query with aggregator.
		// Else, metric query without aggregator is still common.
		// We'll attempt metric query first, but if it can't form one, fall back to IdentLit.
		ex, err := p.tryParseMetricQuery()
		if err == nil && ex != nil {
			return ex, nil
		}
		// fallback literal
		p.l.advance()
		return &IdentLit{Name: t.lit}, nil
	}

	return nil, p.errf(t, "unexpected token %q (expected expression)", t.lit)
}

func (p *Parser) parseFuncCall() (Expr, error) {
	nameTok := p.l.advance() // IDENT
	name := nameTok.lit
	if err := p.expect(tLParen, "expected '(' after function name"); err != nil {
		return nil, err
	}

	args := []Expr{}
	if p.l.peek().typ != tRParen {
		for {
			arg, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			args = append(args, arg)
			if p.l.peek().typ == tComma {
				p.l.advance()
				continue
			}
			break
		}
	}

		if err := p.expect(tRParen, "expected ')' to close function arguments"); err != nil {
			return nil, err
		}
	return &FuncCall{Name: name, Args: args}, nil
}

func (p *Parser) tryParseMetricQuery() (*MetricQuery, error) {
	startPos := p.l.peek().pos

	save := *p.l // shallow copy ok (lexer has no pointers)
	restore := func() { p.l = &save }

	// optional aggregator:
	var agg *string
	if p.l.peek().typ == tIdent && p.peek2().typ == tColon {
		a := p.l.advance().lit
		p.l.advance() // colon
		agg = strPtr(a)
	}

	// metric name must be IDENT
	if p.l.peek().typ != tIdent {
		restore()
		return nil, fmt.Errorf("not a metric query")
	}
	metric := p.l.advance().lit

	mq := &MetricQuery{
		Aggregator: agg,
		Metric:     metric,
	}

	// optional scope
	if p.l.peek().typ == tLBrace {
		scope, err := p.parseScope()
		if err != nil {
			restore()
			return nil, err
		}
		mq.Scope = scope
	}

	// optional group-by: by {a,b,c}
	if p.l.peek().typ == tBy {
		p.l.advance()
		if err := p.expect(tLBrace, "expected '{' after 'by' keyword"); err != nil {
			restore()
			return nil, err
		}
		gb, err := p.parseIdentListUntil(tRBrace)
		if err != nil {
			restore()
			return nil, err
		}
		mq.GroupBy = gb
	}

	// modifiers: .foo(...) .bar(...)
	for p.l.peek().typ == tDot {
		p.l.advance()
		modTok := p.l.peek()
		if modTok.typ != tIdent {
			restore()
			return nil, p.errf(modTok, "expected modifier name after '.'")
		}
		modName := p.l.advance().lit
		if err := p.expect(tLParen, "expected '(' after modifier name"); err != nil {
			restore()
			return nil, err
		}
		args := []Expr{}
		if p.l.peek().typ != tRParen {
			for {
				arg, err := p.parsePrimary() // keep args simple & predictable
				if err != nil {
					restore()
					return nil, err
				}
				args = append(args, arg)
				if p.l.peek().typ == tComma {
					p.l.advance()
					continue
				}
				break
			}
		}
		if err := p.expect(tRParen, "expected ')' to close modifier arguments"); err != nil {
			restore()
			return nil, err
		}
		mq.Modifiers = append(mq.Modifiers, Modifier{Name: modName, Args: args})
	}

	// If we have no aggregator AND no scope/groupby/modifiers, this could be just an identifier literal.
	// We still consider it a MetricQuery only if it's plausible as a metric:
	// metrics often contain at least one dot, but not always. We'll accept as metric query if:
	// - it has an aggregator OR
	// - has scope OR
	// - has groupby OR
	// - has modifiers
	if mq.Aggregator == nil && mq.Scope == nil && len(mq.GroupBy) == 0 && len(mq.Modifiers) == 0 {
		// heuristic: treat as metric query if it contains a dot
		hasDot := false
		for i := 0; i < len(metric); i++ {
			if metric[i] == '.' {
				hasDot = true
				break
			}
		}
		if !hasDot {
			restore()
			return nil, fmt.Errorf("ambiguous: treat as literal")
		}
	}

	// set raw slice
	endPos := p.l.peek().pos
	if endPos < startPos {
		endPos = len(p.src)
	}
	if startPos >= 0 && startPos <= len(p.src) && endPos >= 0 && endPos <= len(p.src) && endPos >= startPos {
		mq.Raw = p.src[startPos:endPos]
	}
	return mq, nil
}

func (p *Parser) parseScope() (TagExpr, error) {
	if err := p.expect(tLBrace, "expected '{'"); err != nil {
		return nil, err
	}

	// Detect boolean-mode vs symbolic-mode by scanning until matching '}'.
	mode := p.detectScopeMode()

	if p.l.peek().typ == tRBrace {
		p.l.advance()
		return nil, nil
	}

	var expr TagExpr
	var err error
	if mode == scopeModeBoolean {
		expr, err = p.parseTagOr()
	} else {
		expr, err = p.parseTagSymbolic()
	}
	if err != nil {
		return nil, err
	}

	if err := p.expect(tRBrace, "expected '}' to close scope"); err != nil {
		return nil, err
	}
	return expr, nil
}

type scopeMode int

const (
	scopeModeSymbolic scopeMode = iota
	scopeModeBoolean
)

func (p *Parser) detectScopeMode() scopeMode {
	// Copy lexer state
	save := *p.l
	defer func() { p.l = &save }()

	for {
		t := p.l.peek()
		if t.typ == tEOF || t.typ == tRBrace {
			return scopeModeSymbolic
		}
		switch t.typ {
		case tAnd, tOr, tNot, tIn, tLParen, tRParen:
			return scopeModeBoolean
		}
		p.l.advance()
	}
}

// ---- Tag boolean parsing: OR -> AND -> NOT -> ATOM ----

func (p *Parser) parseTagOr() (TagExpr, error) {
	left, err := p.parseTagAnd()
	if err != nil {
		return nil, err
	}
	items := []TagExpr{left}
	for p.l.peek().typ == tOr {
		p.l.advance()
		right, err := p.parseTagAnd()
		if err != nil {
			return nil, err
		}
		items = append(items, right)
	}
	if len(items) == 1 {
		return left, nil
	}
	return &TagOr{Items: items}, nil
}

func (p *Parser) parseTagAnd() (TagExpr, error) {
	left, err := p.parseTagNot()
	if err != nil {
		return nil, err
	}
	items := []TagExpr{left}
	for p.l.peek().typ == tAnd {
		p.l.advance()
		right, err := p.parseTagNot()
		if err != nil {
			return nil, err
		}
		items = append(items, right)
	}
	if len(items) == 1 {
		return left, nil
	}
	return &TagAnd{Items: items}, nil
}

func (p *Parser) parseTagNot() (TagExpr, error) {
	if p.l.peek().typ == tNot {
		p.l.advance()
		item, err := p.parseTagNot()
		if err != nil {
			return nil, err
		}
		return &TagNot{Item: item}, nil
	}
	// allow "!" as NOT too
	if p.l.peek().typ == tBang {
		p.l.advance()
		item, err := p.parseTagNot()
		if err != nil {
			return nil, err
		}
		return &TagNot{Item: item}, nil
	}
	return p.parseTagAtom()
}

func (p *Parser) parseTagAtom() (TagExpr, error) {
	t := p.l.peek()

	if t.typ == tLParen {
		p.l.advance()
		ex, err := p.parseTagOr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(tRParen, "expected ')' to close parenthesized tag expression"); err != nil {
			return nil, err
		}
		return ex, nil
	}

	if t.typ != tIdent {
		return nil, p.errf(t, "expected tag term in scope, got %q", t.lit)
	}
	ident := p.l.advance().lit

	// $variable
	if len(ident) > 0 && ident[0] == '$' {
		return &TagTerm{Variable: ident[1:], Raw: ident}, nil
	}

	// key NOT IN (...)
	if p.l.peek().typ == tNot && p.peek2().typ == tIn {
		p.l.advance() // NOT
		p.l.advance() // IN
		vals, err := p.parseParenValueList()
		if err != nil {
			return nil, err
		}
		return &TagIn{Negated: true, Key: ident, Values: vals}, nil
	}

	// key IN (...)
	if p.l.peek().typ == tIn {
		p.l.advance()
		vals, err := p.parseParenValueList()
		if err != nil {
			return nil, err
		}
		return &TagIn{Negated: false, Key: ident, Values: vals}, nil
	}

	// key:value
	if p.l.peek().typ == tColon {
		p.l.advance()
		vTok := p.l.peek()
		if vTok.typ != tIdent && vTok.typ != tString && vTok.typ != tNumber {
			return nil, p.errf(vTok, "expected value (identifier, string, or number) after ':'")
		}
		p.l.advance()
		return &TagTerm{Key: ident, Value: vTok.lit, Raw: ident + ":" + vTok.lit}, nil
	}

	// bare term
	return &TagTerm{Key: ident, Raw: ident}, nil
}

func (p *Parser) parseParenValueList() ([]string, error) {
	if err := p.expect(tLParen, "expected '(' after IN keyword"); err != nil {
		return nil, err
	}
	vals := []string{}
	if p.l.peek().typ != tRParen {
		for {
			t := p.l.peek()
			if t.typ != tIdent && t.typ != tString && t.typ != tNumber {
				return nil, p.errf(t, "expected value (identifier, string, or number) in IN-list")
			}
			vals = append(vals, t.lit)
			p.l.advance()
			if p.l.peek().typ == tComma {
				p.l.advance()
				continue
			}
			break
		}
	}
	if err := p.expect(tRParen, "expected ')' to close IN-list"); err != nil {
		return nil, err
	}
	return vals, nil
}

// ---- Symbolic scope: term (',' term)*, optional leading '!' on term ----
func (p *Parser) parseTagSymbolic() (TagExpr, error) {
	items := []TagExpr{}
	for {
		if p.l.peek().typ == tRBrace {
			break
		}
		neg := false
		if p.l.peek().typ == tBang {
			neg = true
			p.l.advance()
		}
		t := p.l.peek()
		if t.typ != tIdent {
			return nil, p.errf(t, "expected tag term")
		}
		ident := p.l.advance().lit

		// $variable
		if len(ident) > 0 && ident[0] == '$' {
			items = append(items, &TagTerm{Negated: neg, Variable: ident[1:], Raw: ident})
		} else if p.l.peek().typ == tColon {
			p.l.advance()
			vTok := p.l.peek()
			if vTok.typ != tIdent && vTok.typ != tString && vTok.typ != tNumber {
				return nil, p.errf(vTok, "expected value (identifier, string, or number) after ':'")
			}
			p.l.advance()
			items = append(items, &TagTerm{Negated: neg, Key: ident, Value: vTok.lit, Raw: ident + ":" + vTok.lit})
		} else {
			items = append(items, &TagTerm{Negated: neg, Key: ident, Raw: ident})
		}

		if p.l.peek().typ == tComma {
			p.l.advance()
			continue
		}
		break
	}

	if len(items) == 0 {
		return nil, nil
	}
	if len(items) == 1 {
		return items[0], nil
	}
	return &TagAnd{Items: items}, nil
}

// ---- helpers ----

func (p *Parser) parseIdentListUntil(end tokenType) ([]string, error) {
	out := []string{}
	if p.l.peek().typ == end {
		p.l.advance()
		return out, nil
	}
	for {
		t := p.l.peek()
		if t.typ != tIdent {
			return nil, p.errf(t, "expected identifier")
		}
		out = append(out, t.lit)
		p.l.advance()
		if p.l.peek().typ == tComma {
			p.l.advance()
			continue
		}
		break
	}
	delimName := tokenTypeName(end)
	if err := p.expect(end, fmt.Sprintf("expected %s to close list", delimName)); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *Parser) expect(tt tokenType, msg string) error {
	t := p.l.peek()
	if t.typ != tt {
		expectedName := tokenTypeName(tt)
		gotName := tokenTypeName(t.typ)
		if msg != "" {
			return p.errf(t, "%s, got %s %q", msg, gotName, t.lit)
		}
		return p.errf(t, "expected %s, got %s %q", expectedName, gotName, t.lit)
	}
	p.l.advance()
	return nil
}

func (p *Parser) peek2() token {
	save := *p.l
	_ = save.advance()
	return save.peek()
}

func (p *Parser) errf(t token, format string, args ...any) error {
	return &ParseError{
		Pos: t.pos,
		Msg: fmt.Sprintf(format, args...),
		Src: p.src,
	}
}

// tokenTypeName returns a human-readable name for a token type
func tokenTypeName(tt tokenType) string {
	switch tt {
	case tEOF:
		return "end of input"
	case tIdent:
		return "identifier"
	case tNumber:
		return "number"
	case tString:
		return "string"
	case tColon:
		return "':'"
	case tLBrace:
		return "'{'"
	case tRBrace:
		return "'}'"
	case tLParen:
		return "'('"
	case tRParen:
		return "')'"
	case tComma:
		return "','"
	case tDot:
		return "'.'"
	case tBang:
		return "'!'"
	case tBy:
		return "'by'"
	case tAnd:
		return "'and'"
	case tOr:
		return "'or'"
	case tNot:
		return "'not'"
	case tIn:
		return "'in'"
	default:
		return fmt.Sprintf("token type %d", tt)
	}
}
