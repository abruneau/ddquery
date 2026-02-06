# ddquery

A Go parser for Datadog query expressions that converts query strings into a structured Abstract Syntax Tree (AST) for programmatic inspection and manipulation.

## Features

- ✅ **Complete Datadog Query Support**
  - Metric queries with aggregators (`avg`, `sum`, `max`, etc.)
  - **Monitor queries** with timeframes and thresholds (`avg(last_10m):query > 0.95`)
  - **Service check queries** with method chaining (`"metric".over("*").by("tag")`)
  - Tag scopes with boolean operators (`AND`, `OR`, `NOT`)
  - Group-by clauses
  - Function calls and nested functions
  - **Named/keyword arguments** in functions (`direction='both', interval=120`)
  - **Method call chaining** on expressions (`.rollup("count").by("host")`)
  - Modifiers (`fill`, `rollup`, `as_count`, `as_rate`, etc.)
  - Variables (`$var`)
  - IN clauses for tag filtering
  - **Arithmetic expressions** with proper precedence (`+`, `-`, `*`, `/`)
  - **Comparison operators** (`>`, `<`, `>=`, `<=`, `==`, `!=`)
  - **Unary operators** (`-expr`, `+expr`)
  - **Numeric literals** as standalone expressions
  - **Comma-separated expression lists** for multi-series queries
  - **Escaped strings** for complex query patterns

- ✅ **Production Ready**
  - Tested on **7,596 real Datadog queries** from production (100% success rate)
    - 7,217 dashboard metric queries
    - 379 monitor queries
  - Comprehensive error handling with source code context
  - Performance benchmarks included
  - Full API documentation

- ✅ **Developer Friendly**
  - Detailed error messages with position indicators
  - Clean, well-documented code
  - Type-safe AST representation

## Installation

```bash
go get github.com/abruneau/ddquery
```

## Quick Start

```go
package main

import (
    "fmt"
    "log"
    "github.com/abruneau/ddquery"
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

### Monitor Queries

```go
// Monitor query with timeframe and threshold
query, err := ddquery.Parse("avg(last_10m):avg:activemq.artemis.disk_store_usage_pct{*} > 0.95")
if err != nil {
    return err
}

monQuery := query.(*ddquery.MonitorQuery)
fmt.Printf("Timeframe: %s\n", monQuery.Timeframe)          // "avg(last_10m)"
fmt.Printf("Aggregator: %s\n", monQuery.TimeframeAgg)      // "avg"
fmt.Printf("Window: %s\n", monQuery.TimeframeWindow)       // "last_10m"
fmt.Printf("Comparator: %s\n", monQuery.Comparator)        // ">"
fmt.Printf("Threshold: %.2f\n", monQuery.Threshold)        // 0.95

// Access the inner query
innerQuery := monQuery.Query.(*ddquery.MetricQuery)
fmt.Printf("Inner metric: %s\n", innerQuery.Metric)
```

### Service Check Queries

```go
// Service check with method chaining
query, err := ddquery.Parse(`"consul.check".over("*").by("host").last(3).count_by_status()`)
if err != nil {
    return err
}

methodCall := query.(*ddquery.MethodCall)
fmt.Printf("Final method: %s\n", methodCall.Method)  // "count_by_status"

// Walk through the chain
receiver := methodCall.Receiver.(*ddquery.MethodCall)
fmt.Printf("Previous method: %s\n", receiver.Method)  // "last"
```

### Named Arguments in Functions

```go
// Anomaly detection with keyword arguments
query, err := ddquery.Parse(`avg(last_12h):anomalies(avg:metric{*} by {host}, 'agile', 5, direction='both', alert_window='last_15m', interval=120, count_default_zero='true', seasonality='daily') >= 1`)
if err != nil {
    return err
}

monQuery := query.(*ddquery.MonitorQuery)
innerFunc := monQuery.Query.(*ddquery.FuncCall)

// Check for keyword arguments
for _, arg := range innerFunc.Args {
    if kwarg, ok := arg.(*ddquery.KeywordArg); ok {
        fmt.Printf("Keyword: %s = %v\n", kwarg.Key, kwarg.Value)
    }
}
```

### Events and Logs Queries

```go
// Events query with method chaining
query, err := ddquery.Parse(`events("source:windows_crash_detection").rollup("count").by("host").last("10m") > 0`)
if err != nil {
    return err
}

monQuery := query.(*ddquery.MonitorQuery)
// Inner query is a method call chain on a function call

// Logs query with escaped strings
query, err = ddquery.Parse(`logs("source:klaviyo @metric_name:\"Refunded Order\"").index("*").rollup("count").last("1h") > 30`)
if err != nil {
    return err
}
```

### Change Functions

```go
// Change function with two timeframes
query, err := ddquery.Parse(`change(avg(last_5m),last_1d):avg:velero.backup.last_successful_timestamp{*} == 0`)
if err != nil {
    return err
}

monQuery := query.(*ddquery.MonitorQuery)
fmt.Printf("Timeframe function: %s\n", monQuery.Timeframe)  // "change(avg(last_5m),last_1d)"
fmt.Printf("Comparator: %s\n", monQuery.Comparator)         // "=="
fmt.Printf("Threshold: %.0f\n", monQuery.Threshold)         // 0
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
- **`MonitorQuery`**: Represents a monitor query with timeframe, threshold, and comparator
- **`FuncCall`**: Represents a function call expression
- **`MethodCall`**: Represents a method call on an expression (e.g., `.over()`, `.by()`, `.last()`)
- **`DistributionQuery`**: Represents a distribution/histogram query with value filter
- **`BinaryOp`**: Binary arithmetic operation (`+`, `-`, `*`, `/`)
- **`UnaryOp`**: Unary operation (`+expr`, `-expr`)
- **`ExprList`**: Comma-separated list of expressions
- **`KeywordArg`**: Named argument in a function call (e.g., `direction='both'`)
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
go doc github.com/abruneau/ddquery
go doc github.com/abruneau/ddquery Parse
go doc github.com/abruneau/ddquery MetricQuery
go doc github.com/abruneau/ddquery MonitorQuery
```

Or view online at [pkg.go.dev/github.com/abruneau/ddquery](https://pkg.go.dev/github.com/abruneau/ddquery).

For an in-depth explanation of how the parser works internally, what AST nodes are produced for every kind of input, and how edge cases are handled, see the [Parsing Documentation](doc/PARSING.md).

## Performance

The parser is optimized for performance with sub-microsecond parsing times:

```
BenchmarkParse_SimpleQuery-12          6831536    183.1 ns/op    416 B/op    7 allocs/op
BenchmarkParse_ComplexMetricQuery      3999894    298.8 ns/op    560 B/op   12 allocs/op
BenchmarkParse_BooleanScope-12         2250770    534.3 ns/op    880 B/op   25 allocs/op
BenchmarkParse_NestedFunctions         3000000    420.5 ns/op    720 B/op   18 allocs/op
```

Run benchmarks:

```bash
go test -bench=. -benchmem
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

### Monitor Queries

Monitor queries include a timeframe aggregator, the actual query, and a threshold comparison:

```
timeframe_agg(window):query comparator threshold
```

**Patterns:**
1. **Simple timeframe:**
   ```
   avg(last_10m):avg:metric{*} > 0.95
   max(last_5m):sum:metric{*} by {host} >= 100
   ```

2. **Complex functions:**
   ```
   change(avg(last_5m),last_1d):avg:metric{*} == 0
   ```

3. **With method chaining:**
   ```
   avg(last_1h):formula("query").last("5m") > 0.1
   events("source:...").rollup("count").by("host").last("10m") > 0
   ```

**Comparators:**
- `>` Greater than
- `<` Less than
- `>=` Greater than or equal
- `<=` Less than or equal
- `==` Equal to
- `!=` Not equal to

### Service Check Queries

Service checks use quoted metric names with method chaining:

```
"metric.name".over("scope").by("tag1","tag2").last(N).count_by_status()
```

Examples:
- `"consul.check".over("*").by("host").last(3).count_by_status()`
- `"mysql.replication.replica_running".over("*").by("*").last(2).count_by_status()`

### Method Call Chaining

Methods can be chained on any expression using dot notation:

```
expression.method1(args).method2(args).method3(args)
```

Common methods:
- `.over(scope)` - Apply scope filter
- `.by(tag1, tag2, ...)` - Group by tags
- `.last(N)` - Last N values
- `.rollup(aggregator, window)` - Rollup aggregation
- `.index(pattern)` - Index pattern (for logs)
- `.count_by_status()` - Count by status (for service checks)

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
function_name(positional_arg, keyword_arg=value, ...)
```

Functions can be nested and support both positional and named arguments:
```
per_hour(avg:metric.name{env:prod})
outliers(per_hour(avg:metric.name{env:prod}), 'DBSCAN', 3)
anomalies(avg:metric{*}, 'agile', 5, direction='both', interval=120)
```

**Named/Keyword Arguments:**
Functions can accept named arguments for better readability:
```
anomalies(
    query,
    'agile',
    5,
    direction='both',
    alert_window='last_15m',
    interval=120,
    count_default_zero='true',
    seasonality='daily'
)
```

**Special Function Syntax:**
Some functions like `sum()` accept group-by syntax:
```
sum(max:metric{*} by {tag1,tag2}, {tag1, tag2})
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

The parser has been extensively tested on **7,596 real Datadog queries** extracted from production environments, achieving a **100% success rate** and ensuring compatibility with real-world usage patterns.

Test suite includes:
- **7,217 dashboard metric queries** from production dashboards
- **379 monitor queries** from production monitors
- Unit tests for all expression types
- Edge case testing (compact expressions, operator precedence, escaped strings, etc.)
- Error handling validation
- Performance benchmarks

Query coverage:
- ✅ Metric queries with aggregators and modifiers
- ✅ Monitor queries with timeframes and thresholds
- ✅ Service check queries with method chaining
- ✅ Boolean and symbolic tag scopes
- ✅ Function calls with named arguments
- ✅ Arithmetic expressions with proper precedence
- ✅ Events and logs queries
- ✅ Distribution queries
- ✅ Change and anomaly detection functions

Run all tests:

```bash
go test ./...
```

Run tests with coverage:

```bash
go test -cover ./...
```

## Query Types Supported

The parser supports all major Datadog query types:

### 1. Dashboard Metric Queries
Standard metric queries used in dashboards and notebooks:
```
avg:system.cpu.user{env:prod} by {host}
sum:kubernetes.pods.running{*}
avg:metric{*}.fill(zero).rollup(avg, 60).as_count()
```

### 2. Monitor Queries
Alert monitor queries with timeframes and thresholds:
```
avg(last_10m):avg:metric{*} > 0.95
max(last_5m):sum:errors{*} by {service} >= 100
change(avg(last_5m),last_1d):avg:metric{*} == 0
```

### 3. Service Check Queries
Service check monitoring with status aggregation:
```
"consul.check".over("*").by("host").last(3).count_by_status()
"mysql.can_connect".over("*").by("*").last(2).count_by_status()
```

### 4. Events Queries
Event-based monitoring:
```
events("source:windows_crash_detection").rollup("count").by("host").last("10m") > 0
```

### 5. Logs Queries
Log-based monitoring with analytics:
```
logs("source:nginx @http.status_code:>=500").index("*").rollup("count").last("5m") > 10
```

### 6. Formula Queries
Mathematical formulas over queries:
```
formula("(query - query1) / query").last("5m") > 0.1
formula("query * 100 / query1").last("1h") >= 5
```

### 7. Anomaly Detection
Anomaly and outlier detection:
```
avg(last_12h):anomalies(avg:metric{*}, 'agile', 5, direction='both', seasonality='daily') >= 1
outliers(per_hour(avg:metric{*}), 'DBSCAN', 3)
```

### 8. Change Detection
Change-over-time monitoring:
```
change(avg(last_5m),last_1h):metric{*} > 25
change(sum(last_5m),last_1d):metric{*}.as_count() >= 1
```

### 9. Distribution/Histogram Queries
Value distribution monitoring:
```
count(v: v<10):trace.web.request{service:api}
count(v: v>=100):data_streams.latency{*}
```

### 10. Arithmetic Expressions
Complex mathematical calculations:
```
(sum:hits{*} / sum:requests{*}) * 100
avg:used{*} / (avg:used{*} + avg:available{*})
```

## Development

### Project Structure

```
ddquery/
├── ast.go           # AST type definitions
├── lexer.go         # Tokenizer/lexer
├── parser.go        # Parser implementation
├── parser_test.go   # Tests and benchmarks
├── testdata/
│   ├── metric_queries.json   # 7,217 dashboard queries
│   └── monitor_queries.json  # 379 monitor queries
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
