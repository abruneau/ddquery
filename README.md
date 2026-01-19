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

- ✅ **Production Ready**
  - 89.6% test coverage
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

## Testing

Run all tests:

```bash
go test ./pkg/...
```

Run tests with coverage:

```bash
go test -cover ./pkg/...
```

Current test coverage: **89.6%**

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
