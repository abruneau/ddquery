# ddquery

A Go parser for Datadog query expressions that converts query strings into a structured Abstract Syntax Tree (AST) for programmatic inspection and manipulation.

## Features

- ✅ **Complete Datadog Query Support**
  - Metric queries with aggregators (`avg`, `sum`, `max`, etc.)
  - Tag scopes with boolean operators (`AND`, `OR`, `NOT`)
  - Group-by clauses
  - Function calls and nested functions
  - Modifiers (`fill`, `rollup`, `as_count`, `as_rate`, etc.)
  - Variables (`$var`)
  - IN clauses for tag filtering
  - **Arithmetic expressions** with proper precedence (`+`, `-`, `*`, `/`)
  - **Unary operators** (`-expr`, `+expr`)
  - **Numeric literals** as standalone expressions
  - **Comma-separated expression lists** for multi-series queries

- ✅ **Production Ready**
  - Tested on **7,217 real Datadog queries** from production dashboards (100% success rate)
  - Comprehensive error handling with source code context
  - Performance benchmarks included
  - Full API documentation

- ✅ **Developer Friendly**
  - Detailed error messages with position indicators
  - Clean, well-documented code
  - Type-safe AST representation

## Installation

```bash
go get ddquery/pkg
```

## Quick Start

```go
package main

import (
    "fmt"
    "ddquery/pkg"
)

func main() {
    query, err := ddquery.Parse("avg:metric.name{env:prod,service:api} by {host}.as_count()")
    if err != nil {
        log.Fatal(err)
    }

    mq, ok := query.(*ddquery.MetricQuery)
    if !ok {
        log.Fatal("expected metric query")
    }

    fmt.Printf("Aggregator: %s\n", *mq.Aggregator)
    fmt.Printf("Metric: %s\n", mq.Metric)
    fmt.Printf("GroupBy: %v\n", mq.GroupBy)
}
```

## Usage Examples

### Basic Metric Query

```go
query, err := ddquery.Parse("sum:kubernetes.pods.running{env:prod} by {namespace}")
if err != nil {
    return err
}

mq := query.(*ddquery.MetricQuery)
fmt.Printf("%s:%s grouped by %v\n", *mq.Aggregator, mq.Metric, mq.GroupBy)
```

### Boolean Tag Scopes

```go
query, err := ddquery.Parse("sum:kubernetes.pods.running{(env:prd OR env:shd) AND $product}")
if err != nil {
    return err
}

mq := query.(*ddquery.MetricQuery)
// Inspect the tag scope expression
scope := mq.Scope
// scope is a TagExpr (TagAnd, TagOr, TagNot, TagTerm, or TagIn)
```

### Function Calls

```go
query, err := ddquery.Parse("per_hour(avg:metric.name{env:prod} by {host}.as_count())")
if err != nil {
    return err
}

fc := query.(*ddquery.FuncCall)
fmt.Printf("Function: %s with %d arguments\n", fc.Name, len(fc.Args))
```

### Nested Functions

```go
query, err := ddquery.Parse("outliers(per_hour(avg:airflow.job.end{$host} by {job_name,host}.as_count()), 'DBSCAN', 3)")
if err != nil {
    return err
}

fc := query.(*ddquery.FuncCall)
fmt.Printf("Outer function: %s\n", fc.Name)
if len(fc.Args) > 0 {
    nested := fc.Args[0].(*ddquery.FuncCall)
    fmt.Printf("Nested function: %s\n", nested.Name)
}
```

### IN Clauses

```go
query, err := ddquery.Parse("metric{key IN (val1, val2, val3)}")
if err != nil {
    return err
}

mq := query.(*ddquery.MetricQuery)
tagIn := mq.Scope.(*ddquery.TagIn)
fmt.Printf("Key: %s, Values: %v\n", tagIn.Key, tagIn.Values)
```

### Multiple Modifiers

```go
query, err := ddquery.Parse("avg:metric.name{$env, param:value}.fill(zero).rollup(avg, 20).as_count()")
if err != nil {
    return err
}

mq := query.(*ddquery.MetricQuery)
for _, mod := range mq.Modifiers {
    fmt.Printf("Modifier: %s with %d args\n", mod.Name, len(mod.Args))
}
```

### Arithmetic Expressions

```go
// Simple arithmetic with proper precedence
query, err := ddquery.Parse("sum:metric1{*} + sum:metric2{*} * 100")
if err != nil {
    return err
}

binOp := query.(*ddquery.BinaryOp)
fmt.Printf("Operation: %s\n", binOp.Op)  // "+"

// Complex percentage calculations
query, err = ddquery.Parse("(sum:hits{*} / sum:requests{*}) * 100")

// Unary operators
query, err = ddquery.Parse("-sum:errors{*}")
unary := query.(*ddquery.UnaryOp)
fmt.Printf("Unary operation: %s\n", unary.Op)  // "-"
```

### Comma-Separated Expression Lists

```go
// Multiple metrics in a single query
query, err := ddquery.Parse("avg:cpu{host:a}, avg:cpu{host:b}, avg:cpu{host:c}")
if err != nil {
    return err
}

exprList := query.(*ddquery.ExprList)
fmt.Printf("Number of expressions: %d\n", len(exprList.Exprs))
for i, expr := range exprList.Exprs {
    mq := expr.(*ddquery.MetricQuery)
    fmt.Printf("Metric %d: %s\n", i+1, mq.Metric)
}
```

## Error Handling

The parser provides detailed error messages with source code context:

```go
query, err := ddquery.Parse("avg:metric{env:prod} extra")
if err != nil {
    fmt.Println(err.Error())
    // Output:
    // parse error at position 21: unexpected token "extra" after end of expression
    //   vg:metric{env:prod} extra
    //                       ^
}
```

Errors are of type `*ParseError` and include:
- Byte position where the error occurred
- Descriptive error message
- Source code context with visual position indicator

## AST Types

### Expression Types

- **`MetricQuery`**: Represents a metric query with aggregator, scope, group-by, and modifiers
- **`FuncCall`**: Represents a function call expression
- **`DistributionQuery`**: Represents a distribution/histogram query with value filter
- **`BinaryOp`**: Binary arithmetic operation (`+`, `-`, `*`, `/`)
- **`UnaryOp`**: Unary operation (`+expr`, `-expr`)
- **`ExprList`**: Comma-separated list of expressions
- **`StringLit`**: String literal
- **`NumberLit`**: Numeric literal
- **`IdentLit`**: Identifier literal

### Tag Expression Types

- **`TagTerm`**: Single tag key:value pair, bare key, or variable
- **`TagAnd`**: Logical AND of tag expressions
- **`TagOr`**: Logical OR of tag expressions
- **`TagNot`**: Logical NOT of tag expression
- **`TagIn`**: IN clause for matching multiple values

## API Documentation

Full API documentation is available via `go doc`:

```bash
go doc ./pkg
go doc ./pkg Parse
go doc ./pkg MetricQuery
```

Or view online at [pkg.go.dev](https://pkg.go.dev) (when published).

## Performance

The parser is optimized for performance with sub-microsecond parsing times:

```
BenchmarkParse_SimpleQuery-12      6831536    183.1 ns/op    416 B/op    7 allocs/op
BenchmarkParse_ComplexMetricQuery  3999894    298.8 ns/op    560 B/op   12 allocs/op
BenchmarkParse_BooleanScope-12     2250770    534.3 ns/op    880 B/op   25 allocs/op
```

Run benchmarks:

```bash
go test -bench=. -benchmem ./pkg/...
```

## Supported Query Syntax

### Metric Queries

```
[aggregator:]metric.name{[scope]} [by {tag1,tag2,...}] [.modifier1(...) .modifier2(...)]
```

Examples:
- `avg:metric.name{env:prod}`
- `sum:metric.name{env:prod,service:api} by {host}`
- `metric.name{env:prod}.fill(zero).rollup(avg, 20)`

### Arithmetic Expressions

Full arithmetic support with proper operator precedence (multiplication and division before addition and subtraction):

```
expression [operator] expression
```

**Binary operators:**
- `+` Addition
- `-` Subtraction
- `*` Multiplication
- `/` Division

**Unary operators:**
- `-expr` Negation
- `+expr` Positive (identity)

**Examples:**
- `sum:metric1{*} + sum:metric2{*}`
- `(avg:total{*} - avg:used{*}) / avg:total{*} * 100`
- `-sum:errors{*}`
- `100 * (a / b)`

**Precedence rules:**
1. Parentheses (highest)
2. Unary operators (`-`, `+`)
3. Multiplication and division (`*`, `/`)
4. Addition and subtraction (`+`, `-`)
5. Comma-separated lists (lowest)

### Tag Scopes

**Symbolic mode** (comma-separated):
```
{tag1:value1, tag2:value2, !tag3:value3}
```

**Boolean mode** (with operators):
```
{(tag1:value1 OR tag2:value2) AND NOT tag3:value3}
```

**Variables:**
```
{$var1, $var2}
```

**IN clauses:**
```
{key IN (val1, val2, val3)}
{key NOT IN (val1, val2)}
```

### Functions

```
function_name(arg1, arg2, ...)
```

Functions can be nested:
```
per_hour(avg:metric.name{env:prod})
outliers(per_hour(avg:metric.name{env:prod}), 'DBSCAN', 3)
```

### Modifiers

```
.modifier_name(arg1, arg2, ...)
```

Modifiers can be chained:
```
.fill(zero).rollup(avg, 20).as_count()
```

### Expression Lists

Multiple expressions can be separated by commas at the top level:

```
expression1, expression2, expression3, ...
```

Example:
```
avg:cpu{host:a}, avg:cpu{host:b}, avg:memory{host:a}
```

This is useful for querying multiple metrics or series in a single expression.

## Testing

The parser has been extensively tested on **7,217 real Datadog queries** extracted from production dashboards, achieving a **100% success rate** and ensuring compatibility with real-world usage patterns.

Test suite includes:
- 7,217 real-world queries from production dashboards
- Unit tests for all expression types
- Edge case testing (compact expressions, operator precedence, etc.)
- Error handling validation
- Performance benchmarks

Run all tests:

```bash
go test ./...
```

Run tests with coverage:

```bash
go test -cover ./...
```

## Development

### Project Structure

```
ddquery/
├── pkg/
│   ├── ast.go      # AST type definitions
│   ├── lexer.go    # Tokenizer/lexer
│   ├── parser.go   # Parser implementation
│   └── parser_test.go  # Tests and benchmarks
├── go.mod
└── README.md
```

### Code Quality

- ✅ All tests pass
- ✅ `go vet` passes
- ✅ Race detector passes
- ✅ Code formatted with `gofmt`
- ✅ Comprehensive documentation

## License

[Add your license here]

## Contributing

[Add contributing guidelines here]

## Related Projects

- [Datadog API Documentation](https://docs.datadoghq.com/api/latest/metrics/)

---

**Status**: Production-ready ✅

For more information, see the [code review document](CODE_REVIEW.md) for detailed analysis and improvements.
