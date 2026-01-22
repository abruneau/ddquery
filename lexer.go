package ddquery

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type tokenType int

const (
	tEOF tokenType = iota
	tIdent
	tNumber
	tString

	tColon
	tLBrace
	tRBrace
	tLParen
	tRParen
	tComma
	tDot
	tBang

	// arithmetic operators
	tPlus  // '+'
	tMinus // '-'
	tStar  // '*'
	tSlash // '/'

	// keywords (case-insensitive, only for pure-letter identifiers)
	tBy
	tAnd
	tOr
	tNot
	tIn
)

type token struct {
	typ tokenType
	lit string
	pos int // byte offset
}

type lexer struct {
	s   string
	i   int
	n   int
	cur token
}

func newLexer(s string) *lexer {
	l := &lexer{s: s, n: len(s)}
	l.cur = l.nextToken()
	return l
}

func (l *lexer) peek() token { return l.cur }

func (l *lexer) advance() token {
	t := l.cur
	l.cur = l.nextToken()
	return t
}

func (l *lexer) nextToken() token {
	l.skipWS()
	if l.i >= l.n {
		return token{typ: tEOF, pos: l.i}
	}

	ch := l.s[l.i]
	pos := l.i

	switch ch {
	case ':':
		l.i++
		return token{typ: tColon, lit: ":", pos: pos}
	case '{':
		l.i++
		return token{typ: tLBrace, lit: "{", pos: pos}
	case '}':
		l.i++
		return token{typ: tRBrace, lit: "}", pos: pos}
	case '(':
		l.i++
		return token{typ: tLParen, lit: "(", pos: pos}
	case ')':
		l.i++
		return token{typ: tRParen, lit: ")", pos: pos}
	case ',':
		l.i++
		return token{typ: tComma, lit: ",", pos: pos}
	case '.':
		l.i++
		return token{typ: tDot, lit: ".", pos: pos}
	case '!':
		l.i++
		return token{typ: tBang, lit: "!", pos: pos}
	case '+':
		l.i++
		return token{typ: tPlus, lit: "+", pos: pos}
	case '*':
		// Star is tricky: it can be:
		// 1. Part of an identifier: "metric*" or in scope "{*}" (handled by ident lexing)
		// 2. A multiplication operator: "a * b" or "(a)*100"
		// Check if it's a standalone operator
		if l.i+1 >= l.n {
			l.i++
			return token{typ: tStar, lit: "*", pos: pos}
		}
		// Check previous character - if it's a delimiter, treat * as operator
		if pos > 0 {
			prevCh := l.s[pos-1]
			if prevCh == ')' || prevCh == '}' || prevCh == ']' ||
				prevCh == ' ' || prevCh == '\t' || prevCh == '\n' || prevCh == '\r' {
				l.i++
				return token{typ: tStar, lit: "*", pos: pos}
			}
		}
		nextCh := l.s[l.i+1]
		if nextCh == ' ' || nextCh == '\t' || nextCh == '\n' || nextCh == '\r' ||
			nextCh == '(' || nextCh == ')' || nextCh == '{' || nextCh == '}' ||
			nextCh == ',' || nextCh == ':' || nextCh == '.' || nextCh == '+' ||
			nextCh == '-' || nextCh == '*' || nextCh == '/' {
			l.i++
			return token{typ: tStar, lit: "*", pos: pos}
		}
		// Otherwise, it's part of an identifier (fall through to ident lexing)
	case '-':
		// Minus is tricky: it can be:
		// 1. Part of a negative number: "-100" (handled by number lexing below)
		// 2. Part of an identifier: "metric-name" (handled by ident lexing)
		// 3. A unary/binary operator: "a - b" or "-x"
		// We check if it's followed by a digit (negative number case)
		if l.i+1 < l.n && isDigit(l.s[l.i+1]) {
			// Let number lexing handle it (fall through)
			break
		}
		// Check previous character - if it's a delimiter, treat - as operator
		if pos > 0 {
			prevCh := l.s[pos-1]
			if prevCh == ')' || prevCh == '}' || prevCh == ']' ||
				prevCh == ' ' || prevCh == '\t' || prevCh == '\n' || prevCh == '\r' {
				l.i++
				return token{typ: tMinus, lit: "-", pos: pos}
			}
		}
		// Check if it's a standalone operator
		// If at start of token (after whitespace), treat as operator when followed by letter/delimiter
		if l.i+1 >= l.n {
			l.i++
			return token{typ: tMinus, lit: "-", pos: pos}
		}
		nextCh := l.s[l.i+1]
		// If followed by whitespace, delimiter, or letter (unary operator case), it's an operator
		if nextCh == ' ' || nextCh == '\t' || nextCh == '\n' || nextCh == '\r' ||
			nextCh == '(' || nextCh == ')' || nextCh == '{' || nextCh == '}' ||
			nextCh == ',' || nextCh == ':' || nextCh == '.' || nextCh == '+' ||
			nextCh == '-' || nextCh == '*' || nextCh == '/' ||
			((nextCh >= 'a' && nextCh <= 'z') || (nextCh >= 'A' && nextCh <= 'Z') || nextCh == '$') {
			// Followed by letter means unary operator (e.g., "-sum"), not part of identifier
			// Followed by delimiter means binary operator
			l.i++
			return token{typ: tMinus, lit: "-", pos: pos}
		}
		// Otherwise, it's part of an identifier (fall through to ident lexing)
		// This handles cases like "metric-name" where - is in the middle
	case '/':
		// Slash can be:
		// 1. Part of an identifier: "http/status" (handled by ident lexing)
		// 2. A division operator: "a / b" or "(a)/100"
		// Check if it's a standalone operator
		if l.i+1 >= l.n {
			l.i++
			return token{typ: tSlash, lit: "/", pos: pos}
		}
		// Check previous character - if it's a delimiter, treat / as operator
		if pos > 0 {
			prevCh := l.s[pos-1]
			if prevCh == ')' || prevCh == '}' || prevCh == ']' ||
				prevCh == ' ' || prevCh == '\t' || prevCh == '\n' || prevCh == '\r' {
				l.i++
				return token{typ: tSlash, lit: "/", pos: pos}
			}
		}
		nextCh := l.s[l.i+1]
		if nextCh == ' ' || nextCh == '\t' || nextCh == '\n' || nextCh == '\r' ||
			nextCh == '(' || nextCh == ')' || nextCh == '{' || nextCh == '}' ||
			nextCh == ',' || nextCh == ':' || nextCh == '.' || nextCh == '+' ||
			nextCh == '-' || nextCh == '*' || nextCh == '/' {
			l.i++
			return token{typ: tSlash, lit: "/", pos: pos}
		}
		// Otherwise, it's part of an identifier (fall through to ident lexing)
	case '\'':
		// single-quoted string
		l.i++
		start := l.i
		for l.i < l.n && l.s[l.i] != '\'' {
			// no escape handling (Datadog examples typically don't require it)
			l.i++
		}
		if l.i >= l.n {
			return token{typ: tString, lit: l.s[start:], pos: pos} // parser will error on missing close if needed
		}
		lit := l.s[start:l.i]
		l.i++ // consume closing '
		return token{typ: tString, lit: lit, pos: pos}
	case '"':
		// double-quoted string
		l.i++
		start := l.i
		for l.i < l.n && l.s[l.i] != '"' {
			// no escape handling (Datadog examples typically don't require it)
			l.i++
		}
		if l.i >= l.n {
			return token{typ: tString, lit: l.s[start:], pos: pos} // parser will error on missing close if needed
		}
		lit := l.s[start:l.i]
		l.i++ // consume closing "
		return token{typ: tString, lit: lit, pos: pos}
	}

	// lexer.go (replace your "number" branch with this)
	if isDigit(ch) || (ch == '-' && l.i+1 < l.n && isDigit(l.s[l.i+1])) {
		j := l.i
		if l.s[j] == '-' {
			j++
		}
		hasDot := false
		for j < l.n {
			c := l.s[j]
			if c == '.' && !hasDot {
				hasDot = true
				j++
				continue
			}
			if !isDigit(c) { // stop on first non-digit
				break
			}
			j++
		}

		// If the next character continues an identifier (e.g. 5xx),
		// DO NOT emit a number token here; let ident-lexing handle it.
		if j < l.n && isIdentChar(l.s[j]) {
			// fallthrough to ident lexing
		} else if j > l.i {
			lit := l.s[l.i:j]
			l.i = j
			return token{typ: tNumber, lit: lit, pos: pos}
		}
	}

	// identifier-ish: accept lots of characters used in tag values / metric names / vars:
	// letters, digits, '_', '-', '.', '*', '/', '$'
	if isIdentChar(ch) {
		j := l.i
		for j < l.n && isIdentChar(l.s[j]) {
			j++
		}
		lit := l.s[l.i:j]
		l.i = j

		// keywords only for pure letters (so "pod_phase" isn't mistaken as NOT/IN etc.)
		if isAllLetters(lit) {
			switch strings.ToLower(lit) {
			case "by":
				return token{typ: tBy, lit: lit, pos: pos}
			case "and":
				return token{typ: tAnd, lit: lit, pos: pos}
			case "or":
				return token{typ: tOr, lit: lit, pos: pos}
			case "not":
				return token{typ: tNot, lit: lit, pos: pos}
			case "in":
				return token{typ: tIn, lit: lit, pos: pos}
			}
		}

		return token{typ: tIdent, lit: lit, pos: pos}
	}

	// unknown char: return as ident to fail later with a good message
	l.i++
	return token{typ: tIdent, lit: string(ch), pos: pos}
}

func (l *lexer) skipWS() {
	for l.i < l.n {
		if c := l.s[l.i]; c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			l.i++
			continue
		}
		break
	}
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func isIdentChar(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_' || b == '-' || b == '.' || b == '*' || b == '/' || b == '$'
}

func isAllLetters(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return len(s) > 0
}

func parseFloatStrict(lit string) (float64, error) {
	f, err := strconv.ParseFloat(lit, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q: %w", lit, err)
	}
	return f, nil
}
