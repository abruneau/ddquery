# Parsing Documentation

This document provides an exhaustive, detailed explanation of how the `ddquery` parser works internally, what AST nodes it produces for every kind of input, and how edge cases are handled. It is intended as a complete reference for contributors and advanced users.

---

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Lexer (Tokenization)](#lexer-tokenization)
   - [Token Types](#token-types)
   - [Identifier Characters](#identifier-characters)
   - [Operator vs. Identifier Ambiguity](#operator-vs-identifier-ambiguity)
   - [Keywords](#keywords)
   - [String Literals](#string-literals)
   - [Number Literals (Lexer Level)](#number-literals-lexer-level)
3. [Parser (Syntax Analysis)](#parser-syntax-analysis)
   - [Entry Point: `Parse()`](#entry-point-parse)
   - [Operator Precedence](#operator-precedence)
   - [Expression Parsing Chain](#expression-parsing-chain)
4. [AST Node Types and When They Are Returned](#ast-node-types-and-when-they-are-returned)
   - [MetricQuery](#metricquery)
   - [MonitorQuery](#monitorquery)
   - [FuncCall](#funccall)
   - [DistributionQuery](#distributionquery)
   - [MethodCall](#methodcall)
   - [BinaryOp](#binaryop)
   - [UnaryOp](#unaryop)
   - [ExprList](#exprlist)
   - [NumberLit](#numberlit)
   - [StringLit](#stringlit)
   - [IdentLit](#identlit)
   - [KeywordArg](#keywordarg)
5. [Tag Scope Parsing](#tag-scope-parsing)
   - [Scope Mode Detection](#scope-mode-detection)
   - [Symbolic Mode](#symbolic-mode)
   - [Boolean Mode](#boolean-mode)
   - [TagExpr Node Types](#tagexpr-node-types)
6. [Modifier Parsing](#modifier-parsing)
7. [Complete Parsing Flow by Query Type](#complete-parsing-flow-by-query-type)
   - [Simple Metric Queries](#simple-metric-queries)
   - [Metric Queries with Aggregator](#metric-queries-with-aggregator)
   - [Metric Queries with Group-By](#metric-queries-with-group-by)
   - [Metric Queries with Modifiers](#metric-queries-with-modifiers)
   - [Monitor Queries](#monitor-queries)
   - [Service Check Queries](#service-check-queries)
   - [Distribution Queries](#distribution-queries)
   - [Arithmetic Expressions](#arithmetic-expressions)
   - [Function Calls](#function-calls)
   - [Comma-Separated Expression Lists](#comma-separated-expression-lists)
8. [Error Handling](#error-handling)
   - [ParseError Structure](#parseerror-structure)
   - [Common Error Cases](#common-error-cases)
9. [Edge Cases and Special Behavior](#edge-cases-and-special-behavior)
   - [Empty Scopes](#empty-scopes)
   - [Values with Embedded Colons](#values-with-embedded-colons)
   - [Variables in Scopes](#variables-in-scopes)
   - [Negation in Symbolic vs Boolean Mode](#negation-in-symbolic-vs-boolean-mode)
   - [Metric Name Heuristic](#metric-name-heuristic)
   - [Compact Expressions (No Spaces)](#compact-expressions-no-spaces)
10. [Detailed AST Examples](#detailed-ast-examples)

---

## Architecture Overview

The library follows a classic **three-stage** compiler front-end architecture:

```
Input String  -->  Lexer  -->  Token Stream  -->  Parser  -->  AST (Expr)
```

1. **Lexer** (`lexer.go`): Converts the raw query string into a stream of tokens. Each token has a type, a literal string value, and a byte position in the source.
2. **Parser** (`parser.go`): Consumes the token stream using a **recursive descent** strategy and builds an Abstract Syntax Tree (AST).
3. **AST** (`ast.go`): Defines the node types that represent every possible construct in a Datadog query.

The public API consists of a single function:

```go
func Parse(s string) (Expr, error)
```

It returns one of the `Expr`-implementing types (or `*ParseError` on failure).

---

## Lexer (Tokenization)

The lexer is implemented in `lexer.go`. It is a hand-written, single-pass, greedy tokenizer. It maintains a cursor position (`l.i`) and always has the "current" token pre-computed in `l.cur`.

### Token Types

| Token Type | Symbol/Pattern | Description |
|---|---|---|
| `tEOF` | (end) | End of input |
| `tIdent` | `metric.name`, `env`, `$var` | Identifier (letters, digits, `_`, `-`, `.`, `*`, `/`, `$`) |
| `tNumber` | `42`, `3.14`, `-100` | Numeric literal (integer or float) |
| `tString` | `'hello'`, `"world"` | Quoted string literal |
| `tColon` | `:` | Colon separator |
| `tLBrace` | `{` | Left curly brace |
| `tRBrace` | `}` | Right curly brace |
| `tLParen` | `(` | Left parenthesis |
| `tRParen` | `)` | Right parenthesis |
| `tComma` | `,` | Comma separator |
| `tDot` | `.` | Dot separator |
| `tBang` | `!` | Exclamation mark (negation) |
| `tPlus` | `+` | Plus operator |
| `tMinus` | `-` | Minus operator |
| `tStar` | `*` | Star/multiply operator |
| `tSlash` | `/` | Slash/divide operator |
| `tBy` | `by`, `BY` | Group-by keyword (case-insensitive) |
| `tAnd` | `and`, `AND` | Logical AND keyword |
| `tOr` | `or`, `OR` | Logical OR keyword |
| `tNot` | `not`, `NOT` | Logical NOT keyword |
| `tIn` | `in`, `IN` | IN keyword |

Additionally, comparison operators (`>`, `<`, `>=`, `<=`, `==`, `!=`) and the assignment operator (`=`) are emitted as `tIdent` tokens with the operator as their literal value. This allows the parser to handle them contextually rather than giving them dedicated token types.

### Identifier Characters

The following characters are considered valid **identifier characters** and are greedily consumed into a single `tIdent` token:

- Letters: `a-z`, `A-Z`
- Digits: `0-9`
- Special: `_`, `-`, `.`, `*`, `/`, `$`

This means metric names like `kubernetes.pods.running`, `metric-name`, `http/status`, and `$variable` are each lexed as **a single identifier token**.

### Operator vs. Identifier Ambiguity

The characters `-`, `*`, and `/` can appear **both** as arithmetic operators and as parts of identifiers (e.g., `metric-name`, `http/status`, `metric*`). The lexer resolves this ambiguity using **context-sensitive rules** based on surrounding characters:

#### Minus (`-`)

| Context | Token Type | Example |
|---|---|---|
| Followed by a digit | Falls through to number lexing | `-100` becomes `tNumber` with lit `"-100"` |
| Previous character is `)`, `}`, `]`, or whitespace | `tMinus` (operator) | `a) - b` |
| Followed by whitespace, delimiter, or letter | `tMinus` (operator) | `a - b`, `-sum:metric{*}` |
| Otherwise (middle of word) | Part of `tIdent` | `metric-name` |

#### Star (`*`)

| Context | Token Type | Example |
|---|---|---|
| Previous character is `)`, `}`, `]`, or whitespace | `tStar` (operator) | `(a) * b` |
| Followed by whitespace, delimiter, or operator | `tStar` (operator) | `a * b` |
| Otherwise | Part of `tIdent` | `metric*`, `{*}` |

#### Slash (`/`)

| Context | Token Type | Example |
|---|---|---|
| Previous character is `)`, `}`, `]`, or whitespace | `tSlash` (operator) | `(a) / b` |
| Followed by whitespace, delimiter, or operator | `tSlash` (operator) | `a / b` |
| Otherwise | Part of `tIdent` | `http/status` |

**Key takeaway**: Spaces around `-`, `*`, `/` are the primary signal that they are operators rather than parts of identifiers. `metric-name` (no spaces) is a single identifier; `metric - name` (with spaces) is a subtraction.

### Keywords

An identifier token is promoted to a keyword token **only if it consists entirely of letters** (no digits, underscores, dashes, etc.). This prevents false matches like `pod_phase` being mistaken for containing the keyword `not` or `in`.

The check is case-insensitive:

| Keyword | Token | Recognized forms |
|---|---|---|
| `by` | `tBy` | `by`, `BY`, `By` |
| `and` | `tAnd` | `and`, `AND`, `And` |
| `or` | `tOr` | `or`, `OR`, `Or` |
| `not` | `tNot` | `not`, `NOT`, `Not` |
| `in` | `tIn` | `in`, `IN`, `In` |

### String Literals

Both single-quoted (`'...'`) and double-quoted (`"..."`) strings are supported. Escape sequences are handled:

| Escape | Result |
|---|---|
| `\'` | `'` |
| `\"` | `"` |
| `\\` | `\` |
| Other (`\x`) | Preserved as `\x` |

If a closing quote is never found, the lexer still returns a `tString` token with whatever content was collected (the parser will typically fail later with a meaningful error).

### Number Literals (Lexer Level)

A number is recognized when the current character is a digit (or `-` followed by a digit). The lexer consumes:

1. Optional leading `-`
2. Digits
3. Optional single `.` followed by more digits

**Important edge case**: If the characters immediately after the numeric portion are valid identifier characters (e.g., `5xx` or `2xx`), the lexer does **not** emit a number token and instead falls through to identifier lexing. This correctly handles identifiers like `5xx` as a single `tIdent` rather than splitting into `tNumber("5")` + `tIdent("xx")`.

---

## Parser (Syntax Analysis)

The parser is a **recursive descent** parser implemented in `parser.go`. It consumes tokens from the lexer and builds the AST. The parser uses several techniques:

- **Lookahead**: `peek()` looks at the current token without consuming; `peek2()` looks two tokens ahead.
- **Backtracking**: Several `tryParse*` methods save the lexer state and restore it if parsing fails. This allows the parser to attempt multiple interpretations of the input.
- **Left-associative binary operators**: Implemented with `for` loops in `parseAddSub()` and `parseMulDiv()`.

### Entry Point: `Parse()`

The `Parse()` function follows this decision process:

```
1. Try to parse as a MonitorQuery (tryParseMonitorQuery)
   - If successful and followed by EOF → return MonitorQuery
   - If fails → restore lexer state and continue

2. Parse as a normal expression (parseExpr)
   - If followed by EOF → return the expression
   - If followed by comma → already handled by parseExpr as ExprList
   - If followed by anything else → error
```

The monitor query attempt happens **first** because monitor queries look like `timeframe(window):query > threshold`, which could otherwise be partially parsed as a function call.

### Operator Precedence

From **lowest** to **highest** precedence:

| Level | Operators | Associativity | Parser Method |
|---|---|---|---|
| 1 (lowest) | `,` (expression list) | Left | `parseExpr()` |
| 2 | `+`, `-` (binary) | Left | `parseAddSub()` |
| 3 | `*`, `/` (binary) | Left | `parseMulDiv()` |
| 4 | `+`, `-` (unary) | Right (recursive) | `parseUnary()` |
| 5 (highest) | Primaries, parentheses | N/A | `parsePrimary()` |

### Expression Parsing Chain

```
parseExpr()        → parseAddSub() [, parseAddSub() ...]
parseAddSub()      → parseMulDiv() [+/- parseMulDiv() ...]
parseMulDiv()      → parseUnary()  [*// parseUnary() ...]
parseUnary()       → [+/-] parseUnary() | parsePrimary()
parsePrimary()     → (parenthesized) | string | number | funcCall | metricQuery | identLit
parsePostfix(expr) → [.method(args) ...]
```

`parsePrimary()` is where the main dispatch happens. It looks at the current token and decides what to parse:

| Current Token | Next Token | Interpretation |
|---|---|---|
| `(` | any | Parenthesized expression |
| string literal | any | `StringLit` (possibly followed by method calls) |
| number literal | any | `NumberLit` |
| identifier | `(` | Try `DistributionQuery`, then `FuncCall` |
| identifier | `:` | Try `MetricQuery` with aggregator |
| identifier | `{` | Try `MetricQuery` without aggregator |
| identifier | other | Try `MetricQuery`, fall back to `IdentLit` |

---

## AST Node Types and When They Are Returned

### MetricQuery

**Struct definition:**

```go
type MetricQuery struct {
    Aggregator *string    // Optional: "avg", "sum", "max", "min", "count", etc.
    Metric     string     // Required: the metric name (e.g., "kubernetes.pods.running")
    Scope      TagExpr    // Optional: tag filter expression (nil if empty scope or no scope)
    GroupBy    []string   // Optional: list of tag keys to group by
    Modifiers  []Modifier // Optional: chained modifiers (.fill(), .rollup(), .as_count(), etc.)
    Raw        string     // The original source text for this query portion
}
```

**Returned when the input matches the pattern:**

```
[aggregator:]metric.name[{scope}] [by {tag1,tag2,...}] [.modifier(...) ...]
```

**Examples and resulting AST:**

| Input | Aggregator | Metric | Scope | GroupBy | Modifiers |
|---|---|---|---|---|---|
| `avg:metric.name{env:prod}` | `"avg"` | `"metric.name"` | `TagTerm{Key:"env", Value:"prod"}` | `nil` | `nil` |
| `metric.name{*}` | `nil` | `"metric.name"` | `TagTerm{Key:"*"}` | `nil` | `nil` |
| `sum:k8s.pods{env:prod} by {host}` | `"sum"` | `"k8s.pods"` | `TagTerm{Key:"env", Value:"prod"}` | `["host"]` | `nil` |
| `metric.name{}.fill(zero).as_count()` | `nil` | `"metric.name"` | `nil` | `nil` | `[{Name:"fill", Args:[IdentLit("zero")]}, {Name:"as_count", Args:[]}]` |
| `metric.name{}` | `nil` | `"metric.name"` | `nil` | `nil` | `nil` |
| `metric.name by {}` | `nil` | `"metric.name"` | `nil` | `[]` (empty) | `nil` |

**Metric name heuristic**: If an identifier has no aggregator, no scope, no group-by, and no modifiers, it is only treated as a `MetricQuery` if the metric name contains a dot (`.`). Otherwise, it falls back to `IdentLit`. This prevents bare identifiers like `zero` or `avg` from being treated as metric queries.

---

### MonitorQuery

**Struct definition:**

```go
type MonitorQuery struct {
    Timeframe       string  // Full timeframe string: "avg(last_10m)", "change(avg(last_5m),last_1d)"
    TimeframeAgg    string  // Aggregator part: "avg", "max", "change"
    TimeframeWindow string  // Window part: "last_10m", "avg(last_5m),last_1d"
    Query           Expr    // The inner query expression
    Comparator      string  // Comparison operator: ">", ">=", "<", "<=", "==", "!="
    Threshold       float64 // Numeric threshold value
}
```

**Returned when the input matches one of these patterns:**

1. **Timeframe pattern**: `func(args):query comparator number`
2. **Direct comparison pattern**: `query comparator number`

The parser attempts monitor query parsing **first** in the `Parse()` function. It uses backtracking: if the input doesn't match a monitor query pattern, the lexer state is restored and normal expression parsing proceeds.

**Pattern 1: With timeframe prefix**

```
avg(last_10m):avg:metric{*} > 0.95
```

| Field | Value |
|---|---|
| `Timeframe` | `"avg(last_10m)"` |
| `TimeframeAgg` | `"avg"` |
| `TimeframeWindow` | `"last_10m"` |
| `Query` | `MetricQuery{Aggregator:"avg", Metric:"metric", Scope:TagTerm{Key:"*"}}` |
| `Comparator` | `">"` |
| `Threshold` | `0.95` |

**Complex timeframe functions:**

```
change(avg(last_5m),last_1d):avg:metric{*} == 0
```

| Field | Value |
|---|---|
| `Timeframe` | `"change(avg(last_5m),last_1d)"` |
| `TimeframeAgg` | `"change"` |
| `TimeframeWindow` | `"avg(last_5m),last_1d"` |
| `Query` | `MetricQuery{...}` |
| `Comparator` | `"=="` |
| `Threshold` | `0` |

**Pattern 2: Without timeframe (direct comparison)**

```
events("source:crash").rollup("count").last("10m") > 0
formula("(query - query1) / query").last("5m") > 0.1
```

In this case, `Timeframe`, `TimeframeAgg`, and `TimeframeWindow` are all empty strings. The `Query` field holds the entire expression before the comparator.

| Field | Value (for the events example) |
|---|---|
| `Timeframe` | `""` |
| `TimeframeAgg` | `""` |
| `TimeframeWindow` | `""` |
| `Query` | `MethodCall{...}` (chain of `.rollup().last()` on `FuncCall`) |
| `Comparator` | `">"` |
| `Threshold` | `0` |

**When monitor query detection fails**: If there is no comparator operator (`>`, `<`, `>=`, `<=`, `==`, `!=`) followed by a number at the end, the parser restores the lexer state and falls through to normal expression parsing. This means `avg(last_10m):avg:metric{*}` without a comparator would **not** be parsed as a `MonitorQuery` but instead as a `FuncCall` with the rest parsed differently.

---

### FuncCall

**Struct definition:**

```go
type FuncCall struct {
    Name string // Function name
    Args []Expr // Function arguments (can include any Expr type)
}
```

**Returned when the input matches: `identifier(args...)`**

The parser recognizes a function call when an identifier is followed by `(`. Arguments can be:

- Metric queries
- Other function calls (nested)
- String literals
- Number literals
- Identifier literals
- Keyword arguments
- Brace-enclosed tag lists (`{tag1, tag2}`)
- Arithmetic expressions

**Examples:**

| Input | Name | Args |
|---|---|---|
| `func()` | `"func"` | `[]` (empty) |
| `per_hour(avg:metric{*})` | `"per_hour"` | `[MetricQuery{...}]` |
| `outliers(per_hour(avg:m{*}), 'DBSCAN', 3)` | `"outliers"` | `[FuncCall{...}, StringLit{"DBSCAN"}, NumberLit{3}]` |
| `anomalies(avg:m{*}, 'agile', 5, direction='both')` | `"anomalies"` | `[MetricQuery{...}, StringLit{"agile"}, NumberLit{5}, KeywordArg{Key:"direction", Value:StringLit{"both"}}]` |
| `sum(max:m{*} by {tag}, {tag})` | `"sum"` | `[MetricQuery{...}, IdentLit{Name:"{tag}"}]` |

**Keyword arguments** are detected by looking ahead for `IDENT = value`:

```go
// Input: anomalies(query, 'agile', direction='both', interval=120)
//                                   ^^^^^^^^^^^^^^^^  ^^^^^^^^^^^
//                                   KeywordArg        KeywordArg
```

**Brace-enclosed tag lists** in function arguments (e.g., `{tag1, tag2}`) are parsed as `IdentLit` with the entire brace content as the name (e.g., `IdentLit{Name:"{tag1,tag2}"}`).

---

### DistributionQuery

**Struct definition:**

```go
type DistributionQuery struct {
    Function   string       // Always "count" currently
    Comparator string       // "<", "<=", ">", or ">="
    Threshold  float64      // Numeric threshold
    Query      *MetricQuery // The underlying metric query
}
```

**Returned when the input matches: `count(v: v<N):metric{scope}`**

The parser uses `tryParseDistributionQuery()` which looks for the exact pattern:

1. Identifier `count`
2. `(`
3. Identifier (variable name, typically `v`)
4. `:`
5. Optional second `v`
6. Comparison operator (`<`, `<=`, `>`, `>=`)
7. Number
8. `)`
9. `:`
10. A valid metric query

If any of these steps fail, the parser backtracks and tries other interpretations.

**Examples:**

| Input | Function | Comparator | Threshold | Query.Metric |
|---|---|---|---|---|
| `count(v: v<10):trace.web.request{service:api}` | `"count"` | `"<"` | `10` | `"trace.web.request"` |
| `count(v: v>=0):data_streams.latency{direction:in}` | `"count"` | `">="` | `0` | `"data_streams.latency"` |
| `count(v: v>=100):metric{*}.as_count()` | `"count"` | `">="` | `100` | `"metric"` (with modifiers) |

---

### MethodCall

**Struct definition:**

```go
type MethodCall struct {
    Receiver Expr   // The expression the method is called on
    Method   string // Method name
    Args     []Expr // Method arguments
}
```

**Returned when any expression is followed by `.method(args)`.**

Method calls are parsed by `parsePostfix()`, which is called after parsing any primary expression, parenthesized expression, or function call. Method calls can be **chained**, resulting in nested `MethodCall` nodes where each `Receiver` is the previous `MethodCall`.

**Method names can be keywords**: The parser explicitly allows `by`, `and`, `or`, `not`, and `in` as method names, so `.by("host")` works correctly even though `by` is normally a keyword.

**Examples:**

**Service check query:**

```
"consul.check".over("*").by("host").last(3).count_by_status()
```

Produces a deeply nested structure:

```
MethodCall{
    Receiver: MethodCall{
        Receiver: MethodCall{
            Receiver: MethodCall{
                Receiver: StringLit{Value: "consul.check"},
                Method: "over",
                Args: [StringLit{Value: "*"}]
            },
            Method: "by",
            Args: [StringLit{Value: "host"}]
        },
        Method: "last",
        Args: [NumberLit{Value: 3}]
    },
    Method: "count_by_status",
    Args: []
}
```

**Method on function call:**

```
events("source:crash").rollup("count").by("host").last("10m")
```

```
MethodCall{
    Receiver: MethodCall{
        Receiver: MethodCall{
            Receiver: FuncCall{Name: "events", Args: [StringLit{Value: "source:crash"}]},
            Method: "rollup",
            Args: [StringLit{Value: "count"}]
        },
        Method: "by",
        Args: [StringLit{Value: "host"}]
    },
    Method: "last",
    Args: [StringLit{Value: "10m"}]
}
```

---

### BinaryOp

**Struct definition:**

```go
type BinaryOp struct {
    Op    string // "+", "-", "*", or "/"
    Left  Expr   // Left operand
    Right Expr   // Right operand
}
```

**Returned when two expressions are separated by an arithmetic operator.**

Binary operations respect standard mathematical precedence and are **left-associative**.

**Precedence examples:**

| Input | Parsed As | Resulting Tree |
|---|---|---|
| `a + b * c` | `a + (b * c)` | `BinaryOp{"+", IdentLit("a"), BinaryOp{"*", IdentLit("b"), IdentLit("c")}}` |
| `a - b / c` | `a - (b / c)` | `BinaryOp{"-", IdentLit("a"), BinaryOp{"/", IdentLit("b"), IdentLit("c")}}` |
| `(a + b) * c` | `(a + b) * c` | `BinaryOp{"*", BinaryOp{"+", IdentLit("a"), IdentLit("b")}, IdentLit("c")}` |
| `a - b - c` | `(a - b) - c` | `BinaryOp{"-", BinaryOp{"-", IdentLit("a"), IdentLit("b")}, IdentLit("c")}` |
| `a / b / c` | `(a / b) / c` | `BinaryOp{"/", BinaryOp{"/", IdentLit("a"), IdentLit("b")}, IdentLit("c")}` |

**Real-world example:**

```
( ( sum:metric1{*} - sum:metric2{*} ) / sum:metric3{*} ) * 100
```

```
BinaryOp{
    Op: "*",
    Left: BinaryOp{
        Op: "/",
        Left: BinaryOp{
            Op: "-",
            Left: MetricQuery{Aggregator:"sum", Metric:"metric1", Scope:TagTerm{Key:"*"}},
            Right: MetricQuery{Aggregator:"sum", Metric:"metric2", Scope:TagTerm{Key:"*"}}
        },
        Right: MetricQuery{Aggregator:"sum", Metric:"metric3", Scope:TagTerm{Key:"*"}}
    },
    Right: NumberLit{Value: 100}
}
```

---

### UnaryOp

**Struct definition:**

```go
type UnaryOp struct {
    Op   string // "+" or "-"
    Expr Expr   // Operand
}
```

**Returned when an expression is prefixed with `+` or `-` (as a unary operator).**

Unary operators are parsed with right recursion, allowing double negation (`--x`).

**Examples:**

| Input | Resulting AST |
|---|---|
| `-sum:metric{*}` | `UnaryOp{Op:"-", Expr:MetricQuery{...}}` |
| `+100` | `UnaryOp{Op:"+", Expr:NumberLit{100}}` |
| `-func()` | `UnaryOp{Op:"-", Expr:FuncCall{...}}` |
| `--x` | `UnaryOp{Op:"-", Expr:UnaryOp{Op:"-", Expr:IdentLit{"x"}}}` |

**Note**: `-100` as a standalone expression is parsed as `UnaryOp{"-", NumberLit{100}}`, **not** as `NumberLit{-100}`. However, when `-100` appears as a tag value inside a scope (e.g., `{count:-100}`), it is parsed as part of the tag value string.

---

### ExprList

**Struct definition:**

```go
type ExprList struct {
    Exprs []Expr // Two or more expressions
}
```

**Returned when the top-level input contains multiple comma-separated expressions.**

The comma operator has the **lowest** precedence. It is only recognized at the top level of `parseExpr()`. Inside function arguments and scopes, commas serve as argument/term separators instead.

**Examples:**

| Input | Exprs |
|---|---|
| `avg:m1{*}, avg:m2{*}` | `[MetricQuery{...}, MetricQuery{...}]` |
| `expr1, expr2, expr3` | `[IdentLit("expr1"), IdentLit("expr2"), IdentLit("expr3")]` |
| `a + b, c * d` | `[BinaryOp{"+", ...}, BinaryOp{"*", ...}]` |

**Important**: If there is only one expression (no commas), the expression is returned directly, **not** wrapped in an `ExprList`. The `ExprList` type is only used when there are two or more expressions.

---

### NumberLit

**Struct definition:**

```go
type NumberLit struct {
    Value float64
}
```

**Returned when a standalone numeric token appears as an expression.**

The `Value` is always a `float64`, parsed from the token literal using `strconv.ParseFloat`. Both integers and floating-point numbers are supported.

| Input | Value |
|---|---|
| `100` | `100.0` |
| `3.14` | `3.14` |
| `0` | `0.0` |
| `0.95` | `0.95` |

---

### StringLit

**Struct definition:**

```go
type StringLit struct {
    Value string // The string content (without surrounding quotes)
}
```

**Returned when a quoted string appears as an expression.**

String literals are produced when the lexer encounters `'...'` or `"..."`. The `Value` field contains the **unescaped content** (no surrounding quotes).

| Input | Value |
|---|---|
| `'DBSCAN'` | `"DBSCAN"` |
| `"consul.check"` | `"consul.check"` |
| `'it\'s'` | `"it's"` |
| `"escaped \"quotes\""` | `escaped "quotes"` |

**Method calls on strings**: String literals can have method calls chained on them. This is how service check queries work:

```
"consul.check".over("*")
→ MethodCall{Receiver: StringLit{"consul.check"}, Method: "over", Args: [StringLit{"*"}]}
```

---

### IdentLit

**Struct definition:**

```go
type IdentLit struct {
    Name string
}
```

**Returned as a fallback when an identifier cannot be interpreted as a metric query or function call.**

This happens when:
1. The identifier doesn't contain a dot (no metric name heuristic match)
2. The identifier is not followed by `(` (not a function call)
3. The identifier is not followed by `:` or `{` (not a metric query with aggregator or scope)

**Examples:**

| Input | Where it appears | Name |
|---|---|---|
| `zero` (in `fill(zero)`) | Function argument | `"zero"` |
| `avg` (in `rollup(avg, 20)`) | Function argument | `"avg"` |
| `x` (in `a - x`) | Arithmetic operand | `"x"` |
| `{tag1,tag2}` (in `sum(..., {tag1,tag2})`) | Function argument (special) | `"{tag1,tag2}"` |

---

### KeywordArg

**Struct definition:**

```go
type KeywordArg struct {
    Key   string // Argument name
    Value Expr   // Argument value (StringLit, NumberLit, IdentLit, etc.)
}
```

**Returned inside function arguments when the pattern `IDENT = value` is detected.**

The parser looks ahead for `=` after an identifier inside function argument lists. If found, a `KeywordArg` is produced.

**Examples:**

| Input | Key | Value |
|---|---|---|
| `direction='both'` | `"direction"` | `StringLit{"both"}` |
| `interval=120` | `"interval"` | `NumberLit{120}` |
| `count_default_zero='true'` | `"count_default_zero"` | `StringLit{"true"}` |
| `seasonality='daily'` | `"seasonality"` | `StringLit{"daily"}` |

---

## Tag Scope Parsing

Tag scopes appear between `{` and `}` in metric queries. The parser supports two fundamentally different modes for parsing scopes.

### Scope Mode Detection

Before parsing the scope content, the parser scans ahead (without consuming tokens) to determine the mode:

**Boolean mode** is chosen if any of these tokens are found inside the braces:
- `AND`, `OR`, `NOT` keywords
- `IN` keyword
- `(` or `)` parentheses

**Symbolic mode** is the default (no boolean operators found).

This detection is done by `detectScopeMode()`, which saves and restores the lexer state.

### Symbolic Mode

In symbolic mode, tags are separated by commas. Each tag can be:

1. **Key:value pair**: `env:prod`
2. **Bare key**: `env`
3. **Variable**: `$product`
4. **Negated term**: `!env:prod` or `!$var`
5. **Variable with value**: `$routercode:3*`

Multiple terms joined by commas are wrapped in `TagAnd`:

```
{env:prod, service:api, $var}
→ TagAnd{Items: [
    TagTerm{Key:"env", Value:"prod", Raw:"env:prod"},
    TagTerm{Key:"service", Value:"api", Raw:"service:api"},
    TagTerm{Variable:"var", Raw:"$var"}
  ]}
```

A single term is returned directly (not wrapped):

```
{env:prod}
→ TagTerm{Key:"env", Value:"prod", Raw:"env:prod"}
```

**Tag value parsing** is greedy: after the colon, the parser consumes all tokens until it hits a comma, closing brace, closing paren, or a boolean keyword (AND/OR/NOT). This allows tag values to contain colons, dots, dashes, and other special characters:

```
{database_id:blv-shared-services:myconsole-14dc2a48}
→ TagTerm{Key:"database_id", Value:"blv-shared-services:myconsole-14dc2a48"}
```

**Empty values** are supported:

```
{service:}
→ TagTerm{Key:"service", Value:""}
```

### Boolean Mode

In boolean mode, a full boolean expression parser is used with this precedence (lowest to highest):

1. `OR`
2. `AND` (also accepts `,` as AND)
3. `NOT` (also accepts `!`)
4. Atom (tag term, variable, IN clause, or parenthesized expression)

```
{(env:prd OR env:shd) AND NOT pod_phase:running AND $product}
→ TagAnd{Items: [
    TagOr{Items: [
        TagTerm{Key:"env", Value:"prd"},
        TagTerm{Key:"env", Value:"shd"}
    ]},
    TagNot{Item: TagTerm{Key:"pod_phase", Value:"running"}},
    TagTerm{Variable:"product"}
  ]}
```

**Atoms** in boolean mode can be:
- Parenthesized sub-expressions: `(expr OR expr)`
- Key:value pairs: `env:prod`
- Bare keys: `env`
- Variables: `$var`
- Star: `*`
- IN clauses: `key IN (val1, val2)`
- NOT IN clauses: `key NOT IN (val1, val2)`

### TagExpr Node Types

#### TagTerm

```go
type TagTerm struct {
    Negated  bool   // true if prefixed with ! (symbolic mode only)
    Key      string // Tag key (or "*" for wildcard)
    Value    string // Tag value (empty for bare keys)
    Variable string // Variable name without '$' (only set for $var patterns)
    Raw      string // Original source text
}
```

**Usage contexts:**

| Input | Negated | Key | Value | Variable | Raw |
|---|---|---|---|---|---|
| `env:prod` | `false` | `"env"` | `"prod"` | `""` | `"env:prod"` |
| `env` | `false` | `"env"` | `""` | `""` | `"env"` |
| `$product` | `false` | `""` | `""` | `"product"` | `"$product"` |
| `!env:prod` (symbolic) | `true` | `"env"` | `"prod"` | `""` | `"env:prod"` |
| `*` | `false` | `"*"` | `""` | `""` | `"*"` |
| `key:'string val'` | `false` | `"key"` | `"string val"` | `""` | `"key:'string val'"` |

**Important distinction**: In symbolic mode, `!key:value` sets `Negated: true` on the `TagTerm`. In boolean mode, `NOT key:value` produces a `TagNot{Item: TagTerm{...}}` instead. The `Negated` field is only used in symbolic mode.

#### TagAnd

```go
type TagAnd struct {
    Items []TagExpr // Two or more items
}
```

Represents logical AND. Created when:
- Symbolic mode: multiple comma-separated terms
- Boolean mode: terms connected with `AND` keyword or `,`

If only one item would result, it is **unwrapped** and returned directly.

#### TagOr

```go
type TagOr struct {
    Items []TagExpr // Two or more items
}
```

Represents logical OR. Only created in boolean mode when terms are connected with `OR`. If only one item, it is unwrapped.

#### TagNot

```go
type TagNot struct {
    Item TagExpr // The negated expression
}
```

Represents logical NOT. Only created in boolean mode with `NOT` keyword or `!` operator. Can be nested (e.g., `NOT NOT key:value` produces `TagNot{Item: TagNot{Item: TagTerm{...}}}`).

#### TagIn

```go
type TagIn struct {
    Negated bool     // true for NOT IN
    Key     string   // Tag key
    Values  []string // List of values (identifiers, strings, or numbers)
}
```

Only created in boolean mode. The values are stored as strings regardless of whether they were identifiers, quoted strings, or numbers.

| Input | Negated | Key | Values |
|---|---|---|---|
| `key IN (val1, val2, val3)` | `false` | `"key"` | `["val1", "val2", "val3"]` |
| `key NOT IN (val1, val2)` | `true` | `"key"` | `["val1", "val2"]` |
| `key IN ('a', 'b')` | `false` | `"key"` | `["a", "b"]` |
| `key IN (1, 2, 3)` | `false` | `"key"` | `["1", "2", "3"]` |
| `key IN ()` | `false` | `"key"` | `[]` |

---

## Modifier Parsing

Modifiers are chained after metric queries using dot notation. They are parsed **inside `tryParseMetricQuery()`**, not by `parsePostfix()`.

**Struct definition:**

```go
type Modifier struct {
    Name string // Modifier name (e.g., "as_count", "fill", "rollup")
    Args []Expr // Arguments (each parsed with parsePrimary, not full arithmetic)
}
```

**Important**: Modifier arguments are parsed with `parsePrimary()`, not `parseAddSub()`. This means arithmetic expressions are not supported inside modifier arguments. Each argument is either a simple literal (number, string, identifier) or a metric query.

**Common modifiers:**

| Modifier | Typical Arguments | Example |
|---|---|---|
| `.as_count()` | None | `metric{*}.as_count()` |
| `.as_rate()` | None | `metric{*}.as_rate()` |
| `.fill(value)` | IdentLit or NumberLit | `metric{*}.fill(zero)`, `metric{*}.fill(0)` |
| `.rollup(agg, window)` | IdentLit, NumberLit | `metric{*}.rollup(avg, 20)` |

**Chained modifiers:**

```
avg:metric{*}.fill(zero).rollup(avg, 20).as_count()
```

```
MetricQuery{
    Aggregator: "avg",
    Metric: "metric",
    Scope: TagTerm{Key:"*"},
    Modifiers: [
        Modifier{Name:"fill", Args:[IdentLit{Name:"zero"}]},
        Modifier{Name:"rollup", Args:[IdentLit{Name:"avg"}, NumberLit{Value:20}]},
        Modifier{Name:"as_count", Args:[]}
    ]
}
```

**Distinction from MethodCall**: Modifiers are part of the `MetricQuery` struct and are parsed tightly coupled with metric queries. `MethodCall` nodes are general-purpose and can be chained on any expression (strings, function calls, parenthesized expressions). Modifiers only appear on `MetricQuery`.

---

## Complete Parsing Flow by Query Type

### Simple Metric Queries

**Input**: `metric.name{tag:value}`

```
Parse()
  → parseExpr()
    → parseAddSub()
      → parseMulDiv()
        → parseUnary()
          → parsePrimary()
            → tIdent("metric.name"), peek2 ≠ tLParen
            → tryParseMetricQuery()
              → no aggregator (no IDENT:COLON pattern)
              → metric = "metric.name"
              → parseScope() → TagTerm{Key:"tag", Value:"value"}
              → return MetricQuery{Metric:"metric.name", Scope:TagTerm{...}}
```

**Result**: `*MetricQuery`

---

### Metric Queries with Aggregator

**Input**: `avg:metric.name{env:prod}`

```
Parse()
  → tryParseMonitorQuery() → fails (no comparator at end) → restore
  → parseExpr()
    → ... → parsePrimary()
      → tIdent("avg"), peek2 = tColon
      → tryParseMetricQuery()
        → aggregator = "avg", consume ':'
        → metric = "metric.name"
        → parseScope() → TagTerm{Key:"env", Value:"prod"}
        → return MetricQuery{Aggregator:"avg", Metric:"metric.name", ...}
```

**Result**: `*MetricQuery`

---

### Metric Queries with Group-By

**Input**: `sum:k8s.pods{*} by {host, namespace}`

```
tryParseMetricQuery()
  → aggregator = "sum"
  → metric = "k8s.pods"
  → parseScope() → TagTerm{Key:"*"}
  → peek = tBy → consume
  → expect(tLBrace)
  → parseIdentListUntil(tRBrace) → ["host", "namespace"]
  → return MetricQuery{GroupBy: ["host", "namespace"], ...}
```

**Result**: `*MetricQuery` with `GroupBy: ["host", "namespace"]`

---

### Metric Queries with Modifiers

**Input**: `avg:metric{*}.fill(zero).rollup(avg, 20)`

```
tryParseMetricQuery()
  → aggregator = "avg", metric = "metric", scope = TagTerm{Key:"*"}
  → peek = tDot → enter modifier loop
    → consume '.', modName = "fill"
    → expect '(', parsePrimary() → IdentLit{"zero"}, expect ')'
    → Modifier{Name:"fill", Args:[IdentLit{"zero"}]}
  → peek = tDot → continue loop
    → consume '.', modName = "rollup"
    → expect '(', parsePrimary() → IdentLit{"avg"}, comma, parsePrimary() → NumberLit{20}, expect ')'
    → Modifier{Name:"rollup", Args:[IdentLit{"avg"}, NumberLit{20}]}
  → peek ≠ tDot → exit loop
```

**Result**: `*MetricQuery` with `Modifiers` array

---

### Monitor Queries

**Input**: `avg(last_10m):avg:metric{*} > 0.95`

```
Parse()
  → tryParseMonitorQuery()
    → peek = tIdent("avg"), peek2 = tLParen
    → parse function: "avg", consume '('
    → scan for matching ')': finds at position after "last_10m"
    → peek after ')' = tColon → this IS a timeframe function
    → timeframe = "avg(last_10m)", timeframeAgg = "avg", timeframeWindow = "last_10m"
    → consume ':'
    → parseAddSub() → MetricQuery{Aggregator:"avg", Metric:"metric", ...}
    → peek = tIdent(">") → comparator = ">"
    → peek = tNumber("0.95") → threshold = 0.95
    → return MonitorQuery{...}
```

**Result**: `*MonitorQuery`

---

### Service Check Queries

**Input**: `"consul.check".over("*").by("host").last(3).count_by_status()`

```
parsePrimary()
  → tString("consul.check")
  → expr = StringLit{Value:"consul.check"}
  → parsePostfix(expr)
    → tDot → methodName = "over", args = [StringLit{"*"}]
    → expr = MethodCall{Receiver: StringLit{...}, Method:"over", ...}
    → tDot → methodName = "by", args = [StringLit{"host"}]
    → expr = MethodCall{Receiver: MethodCall{...}, Method:"by", ...}
    → tDot → methodName = "last", args = [NumberLit{3}]
    → expr = MethodCall{Receiver: MethodCall{...}, Method:"last", ...}
    → tDot → methodName = "count_by_status", args = []
    → expr = MethodCall{Receiver: MethodCall{...}, Method:"count_by_status", ...}
    → no more dots → return
```

**Result**: `*MethodCall` (deeply nested)

---

### Distribution Queries

**Input**: `count(v: v<10):trace.web.request{service:api}`

```
parsePrimary()
  → tIdent("count"), peek2 = tLParen
  → tryParseDistributionQuery()
    → funcName = "count", consume '('
    → varTok = "v", consume ':'
    → optional second "v" → consume
    → opTok = "<" → comparator = "<"
    → numTok = "10" → threshold = 10
    → consume ')', consume ':'
    → tryParseMetricQuery() → MetricQuery{Metric:"trace.web.request", ...}
    → return DistributionQuery{Function:"count", Comparator:"<", Threshold:10, Query:MetricQuery{...}}
```

**Result**: `*DistributionQuery`

---

### Arithmetic Expressions

**Input**: `(sum:m1{*} - sum:m2{*}) / sum:m3{*} * 100`

```
parseExpr()
  → parseAddSub()
    → parseMulDiv()
      → parseUnary() → parsePrimary()
        → tLParen → parseExpr()
          → parseAddSub()
            → parseMulDiv() → parseUnary() → parsePrimary() → MetricQuery(m1)
            → tMinus
            → parseMulDiv() → parseUnary() → parsePrimary() → MetricQuery(m2)
            → return BinaryOp{"-", MQ(m1), MQ(m2)}
        → expect ')'
      → tSlash
      → parseUnary() → parsePrimary() → MetricQuery(m3)
      → left = BinaryOp{"/", BinaryOp{"-",...}, MQ(m3)}
      → tStar
      → parseUnary() → parsePrimary() → NumberLit{100}
      → left = BinaryOp{"*", BinaryOp{"/", ...}, NumberLit{100}}
```

**Result**: `*BinaryOp` with nested structure

---

### Function Calls

**Input**: `outliers(per_hour(avg:m{*} by {host}.as_count()), 'DBSCAN', 3)`

```
parsePrimary()
  → tIdent("outliers"), peek2 = tLParen
  → parseFuncCall()
    → name = "outliers", consume '('
    → arg1: parseFuncArg() → parseAddSub() → ... → parsePrimary()
      → tIdent("per_hour"), peek2 = tLParen
      → parseFuncCall()
        → name = "per_hour", consume '('
        → arg: parseFuncArg() → MetricQuery{Aggregator:"avg", Metric:"m", GroupBy:["host"], Modifiers:[as_count]}
        → consume ')'
        → return FuncCall{Name:"per_hour", Args:[MetricQuery{...}]}
    → consume ','
    → arg2: parseFuncArg() → StringLit{Value:"DBSCAN"}
    → consume ','
    → arg3: parseFuncArg() → NumberLit{Value:3}
    → consume ')'
    → return FuncCall{Name:"outliers", Args:[FuncCall{...}, StringLit{...}, NumberLit{...}]}
```

**Result**: `*FuncCall` with nested `FuncCall` in args

---

### Comma-Separated Expression Lists

**Input**: `avg:m1{tag:val}, avg:m2{tag:val}`

```
parseExpr()
  → first = parseAddSub() → MetricQuery(m1)
  → peek = tComma
  → enter list loop
    → consume ','
    → next = parseAddSub() → MetricQuery(m2)
    → peek ≠ tComma → exit loop
  → return ExprList{Exprs: [MetricQuery(m1), MetricQuery(m2)]}
```

**Result**: `*ExprList`

---

## Error Handling

### ParseError Structure

```go
type ParseError struct {
    Pos int    // Byte position in the source string
    Msg string // Descriptive error message
    Src string // Full source string (for context display)
}
```

The `Error()` method produces messages with source code context:

```
parse error at position 21: unexpected token "extra" after end of expression
  vg:metric{env:prod} extra
                      ^
```

The context window shows up to 20 characters before and after the error position, with a `^` caret pointing to the exact location.

### Common Error Cases

| Input | Error Message | Cause |
|---|---|---|
| `""` (empty) | `unexpected token "" (expected expression)` | Nothing to parse |
| `avg:metric{env:prod` | `expected '}' to close scope` | Unclosed brace |
| `func(arg` | `expected ')' to close function arguments` | Unclosed parenthesis |
| `avg:metric{env:prod} extra` | `unexpected token "extra" after end of expression` | Trailing tokens |
| `metric.fill(zero` | `expected ')' to close modifier arguments` | Unclosed modifier parens |
| `metric{@invalid}` | `expected tag term` | Invalid character in scope |
| `metric{key IN (val1,val2` | `expected ')' to close IN-list` | Unclosed IN list |
| `metric{key IN (val1,)}` | `expected value ... in IN-list` | Unexpected `)` after comma |
| `metric{(key AND key2:val` | `expected '}' to close scope` | Unclosed boolean scope |

**Error type assertion**: Errors returned by `Parse()` are always of type `*ParseError`, so callers can type-assert to access the `Pos` field for programmatic error handling:

```go
_, err := Parse(input)
if err != nil {
    if pe, ok := err.(*ParseError); ok {
        fmt.Printf("Error at position %d: %s\n", pe.Pos, pe.Msg)
    }
}
```

---

## Edge Cases and Special Behavior

### Empty Scopes

An empty scope `{}` results in `Scope: nil` on the `MetricQuery`:

```go
// Input: "metric.name{}"
// Result: MetricQuery{Metric:"metric.name", Scope:nil}
```

### Values with Embedded Colons

Tag values are parsed greedily, so colons within values are included:

```go
// Input: "avg:gcp.metric{database_id:blv-services:myconsole-14dc2a48}"
// Scope: TagTerm{Key:"database_id", Value:"blv-services:myconsole-14dc2a48"}
```

The greedy parsing continues consuming tokens until it hits a comma, closing brace, closing paren, or boolean keyword.

### Variables in Scopes

Variables (prefixed with `$`) can appear in both symbolic and boolean modes:

```go
// Symbolic: {$product}
// → TagTerm{Variable:"product", Raw:"$product"}

// Boolean: {env:prod AND $product}
// → TagAnd{Items: [TagTerm{Key:"env", Value:"prod"}, TagTerm{Variable:"product"}]}
```

Variables can also have values (treated as key-value pairs):

```go
// Input: {$routercode:3*}
// → TagTerm{Key:"$routercode", Value:"3*", Raw:"$routercode:3*"}
// Note: When a variable has a colon+value, it's treated as a key-value pair, not a variable
```

### Negation in Symbolic vs Boolean Mode

The representation of negation differs between modes:

**Symbolic mode** (`!` prefix):

```go
// Input: metric{!env:prod}
// → TagTerm{Negated:true, Key:"env", Value:"prod"}
```

**Boolean mode** (`NOT` keyword or `!`):

```go
// Input: metric{NOT env:prod}
// → TagNot{Item: TagTerm{Key:"env", Value:"prod"}}

// Input: metric{NOT !env:prod}
// → TagNot{Item: TagNot{Item: TagTerm{Key:"env", Value:"prod"}}}
```

### Metric Name Heuristic

When an identifier has no aggregator, no scope, no group-by, and no modifiers, the parser uses a heuristic to decide between `MetricQuery` and `IdentLit`:

- **Contains a dot** → `MetricQuery` (e.g., `metric.name` → `MetricQuery{Metric:"metric.name"}`)
- **No dot** → `IdentLit` (e.g., `zero` → `IdentLit{Name:"zero"}`)

This heuristic is critical for correctly parsing function arguments like `fill(zero)` where `zero` should be an `IdentLit`, not a `MetricQuery`.

### Compact Expressions (No Spaces)

The lexer's context-sensitive operator detection handles compact expressions:

```go
// (avg:metric{*})*100
// ')' before '*' → '*' is tStar operator
// Result: BinaryOp{"*", MetricQuery{...}, NumberLit{100}}

// (a)-b
// ')' before '-' → '-' is tMinus operator
// Result: BinaryOp{"-", IdentLit("a"), IdentLit("b")}

// metric-name{*}
// 'c' before '-' → '-' is part of identifier
// Result: MetricQuery{Metric:"metric-name", ...}
```

### Empty Group-By

An empty group-by clause `by {}` results in an empty slice (not nil):

```go
// Input: "metric.name by {}"
// Result: MetricQuery{GroupBy: []}  // empty slice, not nil
```

### Mixed Boolean/Symbolic Indicators

When a scope contains both comma-separated terms and boolean operators like `OR`, the scope is detected as boolean mode because `OR` is found during the scan:

```go
// Input: metric{model:text-ada-001 or model:ada, $var}
// Mode detected: boolean (because 'or' keyword found)
// Parsed with boolean parser: OR takes precedence, comma acts as AND
```

### Star (`*`) in Scopes

The wildcard `*` is handled specially:

- **Symbolic mode**: `{*}` → `TagTerm{Key:"*"}`
- **Boolean mode**: `{* AND key:value}` → `TagAnd{Items: [TagTerm{Key:"*"}, TagTerm{...}]}`

### Quoted String Values in Scopes

String literals can appear as tag values:

```go
// Input: metric{topic:"datapipeline-r2a-common*"}
// Scope: TagTerm{Key:"topic", Value:"datapipeline-r2a-common*"}
// Note: the string value includes the wildcard since it was inside quotes
```

---

## Detailed AST Examples

### Example 1: Full Metric Query

**Input:**

```
avg:metric.name{$env, param:value}.fill(zero).rollup(avg, 20)
```

**AST:**

```go
&MetricQuery{
    Aggregator: strPtr("avg"),
    Metric:     "metric.name",
    Scope: &TagAnd{
        Items: []TagExpr{
            &TagTerm{Variable: "env", Raw: "$env"},
            &TagTerm{Key: "param", Value: "value", Raw: "param:value"},
        },
    },
    GroupBy: nil,
    Modifiers: []Modifier{
        {Name: "fill", Args: []Expr{&IdentLit{Name: "zero"}}},
        {Name: "rollup", Args: []Expr{&IdentLit{Name: "avg"}, &NumberLit{Value: 20}}},
    },
    Raw: "avg:metric.name{$env, param:value}.fill(zero).rollup(avg, 20)",
}
```

### Example 2: Complex Boolean Scope

**Input:**

```
sum:kubernetes_state.pod.status_phase{(env:prd OR env:shd) AND NOT pod_phase:running AND NOT pod_phase:succeeded AND $product} by {pod_phase,kube_namespace}
```

**AST:**

```go
&MetricQuery{
    Aggregator: strPtr("sum"),
    Metric:     "kubernetes_state.pod.status_phase",
    Scope: &TagAnd{
        Items: []TagExpr{
            &TagOr{
                Items: []TagExpr{
                    &TagTerm{Key: "env", Value: "prd", Raw: "env:prd"},
                    &TagTerm{Key: "env", Value: "shd", Raw: "env:shd"},
                },
            },
            &TagNot{Item: &TagTerm{Key: "pod_phase", Value: "running", Raw: "pod_phase:running"}},
            &TagNot{Item: &TagTerm{Key: "pod_phase", Value: "succeeded", Raw: "pod_phase:succeeded"}},
            &TagTerm{Variable: "product", Raw: "$product"},
        },
    },
    GroupBy: []string{"pod_phase", "kube_namespace"},
    Modifiers: nil,
}
```

### Example 3: Nested Functions with Keyword Args

**Input:**

```
avg(last_12h):anomalies(avg:metric{*} by {host}, 'agile', 5, direction='both', interval=120) >= 1
```

**AST:**

```go
&MonitorQuery{
    Timeframe:       "avg(last_12h)",
    TimeframeAgg:    "avg",
    TimeframeWindow: "last_12h",
    Query: &FuncCall{
        Name: "anomalies",
        Args: []Expr{
            &MetricQuery{
                Aggregator: strPtr("avg"),
                Metric:     "metric",
                Scope:      &TagTerm{Key: "*"},
                GroupBy:    []string{"host"},
            },
            &StringLit{Value: "agile"},
            &NumberLit{Value: 5},
            &KeywordArg{Key: "direction", Value: &StringLit{Value: "both"}},
            &KeywordArg{Key: "interval", Value: &NumberLit{Value: 120}},
        },
    },
    Comparator: ">=",
    Threshold:  1,
}
```

### Example 4: Arithmetic Percentage Calculation

**Input:**

```
100 * ( avg:varnish.cache_hit{$scope} / ( avg:varnish.cache_hit{$scope} + avg:varnish.cache_miss{$scope} ) )
```

**AST:**

```go
&BinaryOp{
    Op: "*",
    Left: &NumberLit{Value: 100},
    Right: &BinaryOp{
        Op: "/",
        Left: &MetricQuery{
            Aggregator: strPtr("avg"),
            Metric:     "varnish.cache_hit",
            Scope:      &TagTerm{Variable: "scope"},
        },
        Right: &BinaryOp{
            Op: "+",
            Left: &MetricQuery{
                Aggregator: strPtr("avg"),
                Metric:     "varnish.cache_hit",
                Scope:      &TagTerm{Variable: "scope"},
            },
            Right: &MetricQuery{
                Aggregator: strPtr("avg"),
                Metric:     "varnish.cache_miss",
                Scope:      &TagTerm{Variable: "scope"},
            },
        },
    },
}
```

### Example 5: Service Check with Full Method Chain

**Input:**

```
"mysql.replication.replica_running".over("*").by("*").last(2).count_by_status()
```

**AST:**

```go
&MethodCall{
    Receiver: &MethodCall{
        Receiver: &MethodCall{
            Receiver: &MethodCall{
                Receiver: &StringLit{Value: "mysql.replication.replica_running"},
                Method:   "over",
                Args:     []Expr{&StringLit{Value: "*"}},
            },
            Method: "by",
            Args:   []Expr{&StringLit{Value: "*"}},
        },
        Method: "last",
        Args:   []Expr{&NumberLit{Value: 2}},
    },
    Method: "count_by_status",
    Args:   []Expr{},
}
```

### Example 6: Distribution Query inside a Monitor

**Input:**

```
avg(last_5m):count(v: v<10):trace.web.request{service:api}.as_count() > 100
```

**AST:**

```go
&MonitorQuery{
    Timeframe:       "avg(last_5m)",
    TimeframeAgg:    "avg",
    TimeframeWindow: "last_5m",
    Query: &DistributionQuery{
        Function:   "count",
        Comparator: "<",
        Threshold:  10,
        Query: &MetricQuery{
            Metric: "trace.web.request",
            Scope:  &TagTerm{Key: "service", Value: "api"},
            Modifiers: []Modifier{
                {Name: "as_count", Args: []Expr{}},
            },
        },
    },
    Comparator: ">",
    Threshold:  100,
}
```

### Example 7: Expression List

**Input:**

```
avg:cpu{host:a}, avg:memory{host:a}, avg:disk{host:a}
```

**AST:**

```go
&ExprList{
    Exprs: []Expr{
        &MetricQuery{Aggregator: strPtr("avg"), Metric: "cpu", Scope: &TagTerm{Key: "host", Value: "a"}},
        &MetricQuery{Aggregator: strPtr("avg"), Metric: "memory", Scope: &TagTerm{Key: "host", Value: "a"}},
        &MetricQuery{Aggregator: strPtr("avg"), Metric: "disk", Scope: &TagTerm{Key: "host", Value: "a"}},
    },
}
```

### Example 8: IN and NOT IN Clauses

**Input:**

```
metric{env IN (prod, staging, dev) AND region NOT IN (us-east, eu-west)}
```

**AST:**

```go
&MetricQuery{
    Metric: "metric",
    Scope: &TagAnd{
        Items: []TagExpr{
            &TagIn{Negated: false, Key: "env", Values: []string{"prod", "staging", "dev"}},
            &TagIn{Negated: true, Key: "region", Values: []string{"us-east", "eu-west"}},
        },
    },
}
```

### Example 9: Logs Query with Method Chaining and Monitor

**Input:**

```
logs("source:klaviyo @metric_name:\"Refunded Order\"").index("*").rollup("count").last("1h") > 30
```

**AST:**

```go
&MonitorQuery{
    Timeframe:    "",
    TimeframeAgg: "",
    Query: &MethodCall{
        Receiver: &MethodCall{
            Receiver: &MethodCall{
                Receiver: &FuncCall{
                    Name: "logs",
                    Args: []Expr{&StringLit{Value: "source:klaviyo @metric_name:\"Refunded Order\""}},
                },
                Method: "index",
                Args:   []Expr{&StringLit{Value: "*"}},
            },
            Method: "rollup",
            Args:   []Expr{&StringLit{Value: "count"}},
        },
        Method: "last",
        Args:   []Expr{&StringLit{Value: "1h"}},
    },
    Comparator: ">",
    Threshold:  30,
}
```

### Example 10: Unary Minus on Complex Expression

**Input:**

```
-sum:nginx.ssl.handshakes_failed_count{*} by {host}.as_count()
```

**AST:**

```go
&UnaryOp{
    Op: "-",
    Expr: &MetricQuery{
        Aggregator: strPtr("sum"),
        Metric:     "nginx.ssl.handshakes_failed_count",
        Scope:      &TagTerm{Key: "*"},
        GroupBy:    []string{"host"},
        Modifiers:  []Modifier{{Name: "as_count", Args: []Expr{}}},
    },
}
```

---

## Summary: Return Type Decision Tree

```
Parse(input) returns:

├─ Is it a monitor query? (has comparator + threshold at end)
│  └─ YES → *MonitorQuery
│
├─ Does the top level have commas between expressions?
│  └─ YES → *ExprList
│
├─ Does the expression start with + or - as unary operator?
│  └─ YES → *UnaryOp
│
├─ Are there binary arithmetic operators (+, -, *, /)?
│  └─ YES → *BinaryOp
│
├─ Is it a parenthesized expression?
│  └─ Returns whatever is inside the parens (unwrapped)
│
├─ Is it a quoted string?
│  ├─ Followed by .method()? → *MethodCall
│  └─ Otherwise → *StringLit
│
├─ Is it a number?
│  └─ YES → *NumberLit
│
├─ Is it an identifier?
│  ├─ Looks like count(v: v<N):metric? → *DistributionQuery
│  ├─ Followed by (? → *FuncCall (possibly with .method() → *MethodCall)
│  ├─ Looks like [agg:]metric{scope}? → *MetricQuery
│  └─ Fallback → *IdentLit
│
└─ None of the above → *ParseError
```
