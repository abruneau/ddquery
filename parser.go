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
//   - Arithmetic expressions with proper precedence (+, -, *, /)
//   - Unary operators (+expr, -expr)
//   - Numeric literals as standalone expressions
//   - Comma-separated expression lists
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
// Arithmetic example:
//
//	query, err := Parse("(sum:hits{*} / sum:requests{*}) * 100")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	binOp, ok := query.(*BinaryOp)
//	if ok {
//	    fmt.Printf("Operation: %s\n", binOp.Op)
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
//   - An Expr representing the parsed query
//   - An error if the input is invalid
//
// Returned expression types:
//   - MetricQuery: Metric query with aggregator, scope, group-by, and modifiers
//   - FuncCall: Function call expression
//   - BinaryOp: Arithmetic operation (+, -, *, /)
//   - UnaryOp: Unary operation (+expr, -expr)
//   - ExprList: Comma-separated list of expressions
//   - NumberLit, StringLit, IdentLit: Literal values
//
// Supported query formats:
//
//   - Metric queries: "avg:metric.name{env:prod} by {host}.as_count()"
//   - Function calls: "per_hour(avg:metric.name{env:prod})"
//   - Nested functions: "outliers(per_hour(avg:metric.name{env:prod}), 'DBSCAN', 3)"
//   - Boolean scopes: "metric{(env:prd OR env:shd) AND $product}"
//   - IN clauses: "metric{key IN (val1, val2, val3)}"
//   - Arithmetic: "(sum:metric1{*} - sum:metric2{*}) / sum:metric3{*} * 100"
//   - Expression lists: "metric1{*}, metric2{*}, metric3{*}"
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
		// Allow comma at top level for expression lists
		if t.typ == tComma {
			// This should have been handled by parseExpr, but if we get here,
			// it means there's a trailing comma or something unexpected
			return nil, p.errf(t, "unexpected token %q after end of expression", t.lit)
		}
		return nil, p.errf(t, "unexpected token %q after end of expression", t.lit)
	}
	return ex, nil
}

// parseExpr handles comma-separated expression lists (lowest precedence).
// If there's only one expression, it returns that expression directly.
// If there are multiple comma-separated expressions, it returns an ExprList.
func (p *Parser) parseExpr() (Expr, error) {
	first, err := p.parseAddSub()
	if err != nil {
		return nil, err
	}

	// Check for comma-separated list
	if p.l.peek().typ == tComma {
		exprs := []Expr{first}
		for p.l.peek().typ == tComma {
			p.l.advance() // consume comma
			next, err := p.parseAddSub()
			if err != nil {
				return nil, err
			}
			exprs = append(exprs, next)
		}
		return &ExprList{Exprs: exprs}, nil
	}

	return first, nil
}

// parseAddSub handles addition and subtraction (left-associative).
func (p *Parser) parseAddSub() (Expr, error) {
	left, err := p.parseMulDiv()
	if err != nil {
		return nil, err
	}

	for {
		t := p.l.peek()
		if t.typ != tPlus && t.typ != tMinus {
			break
		}
		op := t.lit
		p.l.advance() // consume operator

		right, err := p.parseMulDiv()
		if err != nil {
			return nil, err
		}

		left = &BinaryOp{Op: op, Left: left, Right: right}
	}

	return left, nil
}

// parseMulDiv handles multiplication and division (left-associative).
func (p *Parser) parseMulDiv() (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}

	for {
		t := p.l.peek()
		if t.typ != tStar && t.typ != tSlash {
			break
		}
		op := t.lit
		p.l.advance() // consume operator

		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}

		left = &BinaryOp{Op: op, Left: left, Right: right}
	}

	return left, nil
}

// parseUnary handles unary operators: +expr, -expr
func (p *Parser) parseUnary() (Expr, error) {
	t := p.l.peek()
	if t.typ == tPlus || t.typ == tMinus {
		op := t.lit
		p.l.advance() // consume operator
		expr, err := p.parseUnary() // recursive for cases like --x
		if err != nil {
			return nil, err
		}
		return &UnaryOp{Op: op, Expr: expr}, nil
	}
	return p.parsePrimary()
}

func (p *Parser) parsePrimary() (Expr, error) {
	t := p.l.peek()

	// parenthesized expression (supports full arithmetic expressions)
	if t.typ == tLParen {
		p.l.advance()
		ex, err := p.parseExpr() // parseExpr handles full arithmetic with precedence
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

	// IDENT: could be funcCall, metricQuery, distribution query, or ident literal (e.g. "zero" in fill(zero))
	if t.typ == tIdent {
		// Check for distribution query pattern: count(v: v<10):metric{...}
		if distQuery, err := p.tryParseDistributionQuery(); err == nil && distQuery != nil {
			return distQuery, nil
		}

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

func (p *Parser) tryParseDistributionQuery() (*DistributionQuery, error) {
	// Save lexer state for rollback
	save := *p.l
	restore := func() { p.l = &save }

	// Check for pattern: count(v: v<10):metric{...}
	funcTok := p.l.peek()
	if funcTok.typ != tIdent || funcTok.lit != "count" {
		restore()
		return nil, nil
	}
	funcName := funcTok.lit
	p.l.advance() // consume "count"

	// Must have '('
	if p.l.peek().typ != tLParen {
		restore()
		return nil, nil
	}
	p.l.advance() // consume '('

	// Must have identifier followed by ':'
	// Pattern is: v: v<10 or v: v>=0
	varTok := p.l.peek()
	if varTok.typ != tIdent {
		restore()
		return nil, nil
	}
	p.l.advance() // consume first identifier (typically "v")

	if p.l.peek().typ != tColon {
		restore()
		return nil, nil
	}
	p.l.advance() // consume ':'

	// Now we should have: v<10, v>=0, etc.
	// Skip optional second 'v'
	if p.l.peek().typ == tIdent && p.l.peek().lit == "v" {
		p.l.advance()
	}

	// Get comparison operator
	opTok := p.l.peek()
	if opTok.typ != tIdent || (opTok.lit != "<" && opTok.lit != ">") {
		restore()
		return nil, nil
	}
	opLit := opTok.lit
	p.l.advance() // consume < or >

	// Check for optional '=' for <= or >=
	comparator := opLit
	if p.l.peek().typ == tIdent && p.l.peek().lit == "=" {
		comparator = opLit + "="
		p.l.advance() // consume '='
	}

	// Get the threshold number
	numTok := p.l.peek()
	if numTok.typ != tNumber {
		restore()
		return nil, nil
	}
	threshold, err := parseFloatStrict(numTok.lit)
	if err != nil {
		restore()
		return nil, nil
	}
	p.l.advance() // consume number

	// Must have ')'
	if p.l.peek().typ != tRParen {
		restore()
		return nil, nil
	}
	p.l.advance() // consume ')'

	// Must have ':' before metric query
	if p.l.peek().typ != tColon {
		restore()
		return nil, nil
	}
	p.l.advance() // consume ':'

	// Parse the metric query
	mq, err := p.tryParseMetricQuery()
	if err != nil || mq == nil {
		restore()
		return nil, nil
	}

	return &DistributionQuery{
		Function:   funcName,
		Comparator: comparator,
		Threshold:  threshold,
		Query:      mq,
	}, nil
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
			// Use parseAddSub() instead of parseExpr() to avoid creating ExprList
			// Function arguments are already comma-separated by the loop here
			arg, err := p.parseAddSub()
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
	// In boolean mode, accept both 'AND' keyword and comma as AND operators
	for p.l.peek().typ == tAnd || p.l.peek().typ == tComma {
		p.l.advance() // consume AND or comma
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

	// Allow * as a tag term (for {* AND ...} patterns in boolean mode)
	if t.typ != tIdent && t.typ != tStar {
		return nil, p.errf(t, "expected tag term in scope, got %q", t.lit)
	}
	var ident string
	if t.typ == tStar {
		ident = "*"
		p.l.advance()
	} else {
		ident = p.l.advance().lit
	}

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

	// key:value (greedy parsing to handle colons, dots, etc. in values)
	if p.l.peek().typ == tColon {
		p.l.advance()                                 // consume ':'
		value, rawValue, err := p.parseTagValue(true) // boolean mode
		if err != nil {
			return nil, err
		}
		raw := ident + ":" + rawValue
		return &TagTerm{Key: ident, Value: value, Raw: raw}, nil
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
	for p.l.peek().typ != tRBrace {
		neg := false
		if p.l.peek().typ == tBang {
			neg = true
			p.l.advance()
		}
		t := p.l.peek()
		// Allow * as a tag term (for {*} meaning "match all")
		if t.typ != tIdent && t.typ != tStar {
			return nil, p.errf(t, "expected tag term")
		}
		var ident string
		if t.typ == tStar {
			ident = "*"
			p.l.advance()
		} else {
			ident = p.l.advance().lit
		}

		// Check for key:value first (even for $variables, they can have values like $routercode:3*)
		if p.l.peek().typ == tColon {
			p.l.advance()                                  // consume ':'
			value, rawValue, err := p.parseTagValue(false) // symbolic mode
			if err != nil {
				return nil, err
			}
			raw := ident + ":" + rawValue
			// If identifier starts with $, it could be a variable or a key
			// If it has a value after :, treat as key-value pair
			if len(ident) > 0 && ident[0] == '$' {
				items = append(items, &TagTerm{Negated: neg, Key: ident, Value: value, Raw: raw})
			} else {
				items = append(items, &TagTerm{Negated: neg, Key: ident, Value: value, Raw: raw})
			}
		} else {
			// No colon - could be $variable or bare key
			if len(ident) > 0 && ident[0] == '$' {
				items = append(items, &TagTerm{Negated: neg, Variable: ident[1:], Raw: ident})
			} else {
				items = append(items, &TagTerm{Negated: neg, Key: ident, Raw: ident})
			}
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

// parseTagValue greedily consumes tokens to form a tag value until a delimiter is encountered.
// Delimiters are: comma, closing brace, closing paren, or AND/OR/NOT (always check for boolean ops).
// Returns the value string and the raw source text.
func (p *Parser) parseTagValue(inBooleanMode bool) (value string, raw string, err error) {
	// Check if we have an empty value (next token is a delimiter)
	// Always check for boolean operators, even in symbolic mode, as they indicate mode switch
	next := p.l.peek()
	if next.typ == tComma || next.typ == tRBrace || next.typ == tRParen ||
		next.typ == tAnd || next.typ == tOr || next.typ == tNot {
		// Empty value
		return "", "", nil
	}

	// Find the end position by looking ahead for delimiter tokens
	// Save lexer state to peek ahead without consuming
	save := *p.l
	var endPos int

	// Scan ahead to find where the next delimiter token starts
	// Always check for boolean operators as they indicate a delimiter
	for {
		t := p.l.peek()
		if t.typ == tComma || t.typ == tRBrace || t.typ == tRParen ||
			t.typ == tAnd || t.typ == tOr || t.typ == tNot ||
			t.typ == tEOF {
			endPos = t.pos
			break
		}
		p.l.advance()
	}

	// Restore lexer state
	p.l = &save

	// Now consume tokens and build value until we reach endPos
	startPos := next.pos
	var tokens []token
	var lastTokenEnd int

	for {
		t := p.l.peek()
		// Stop if we've reached or passed the end position
		if t.pos >= endPos || t.typ == tEOF {
			break
		}
		// Check for delimiter tokens (always check boolean ops)
		if t.typ == tComma || t.typ == tRBrace || t.typ == tRParen ||
			t.typ == tAnd || t.typ == tOr || t.typ == tNot {
			break
		}
		// Consume token
		consumed := p.l.advance()
		tokens = append(tokens, consumed)
		lastTokenEnd = consumed.pos + len(consumed.lit)
	}

	if len(tokens) == 0 {
		return "", "", nil
	}

	// Extract raw text from source
	if startPos < 0 || startPos > len(p.src) {
		startPos = 0
	}
	if lastTokenEnd > len(p.src) {
		lastTokenEnd = len(p.src)
	}
	if lastTokenEnd < startPos {
		lastTokenEnd = startPos
	}
	raw = p.src[startPos:lastTokenEnd]

	// Build value by concatenating token literals
	// This handles cases like "blv-prd:mysql" where colon is a separate token
	parts := make([]string, len(tokens))
	for i, tok := range tokens {
		parts[i] = tok.lit
	}
	value = strings.Join(parts, "")

	return value, raw, nil
}

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
	case tPlus:
		return "'+'"
	case tMinus:
		return "'-'"
	case tStar:
		return "'*'"
	case tSlash:
		return "'/'"
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
