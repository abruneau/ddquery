package ddquery

// Expr represents any expression in the Datadog query AST.
//
// The following types implement Expr:
//   - MetricQuery: A metric query with aggregator, scope, and modifiers
//   - FuncCall: A function call expression
//   - DistributionQuery: A distribution query with value filter
//   - StringLit: A string literal
//   - NumberLit: A numeric literal
//   - IdentLit: An identifier literal
//   - BinaryOp: A binary arithmetic operation (+, -, *, /)
//   - UnaryOp: A unary operation (+expr, -expr)
//   - ExprList: A comma-separated list of expressions
type Expr interface {
	isExpr()
}

// MetricQuery represents a Datadog metric query.
//
// Example: "avg:metric.name{env:prod} by {host}.as_count()"
//
// Fields:
//   - Aggregator: Optional aggregator function (e.g., "avg", "sum", "max")
//   - Metric: The metric name (e.g., "metric.name")
//   - Scope: Optional tag filter expression (TagExpr)
//   - GroupBy: Optional list of tag keys to group by
//   - Modifiers: Optional list of modifiers (e.g., as_count, fill, rollup)
//   - Raw: The original source text for this query (for debugging/reconstruction)
type MetricQuery struct {
	Aggregator *string    // Optional aggregator (avg, sum, max, etc.)
	Metric     string     // Metric name
	Scope      TagExpr    // Optional tag filter scope
	GroupBy    []string   // Optional group-by tag keys
	Modifiers  []Modifier // Optional modifiers
	Raw        string     // Original source text
}

func (*MetricQuery) isExpr() {}

// FuncCall represents a function call expression.
//
// Example: "per_hour(avg:metric.name{env:prod})"
//
// Functions can be nested and can take metric queries, literals, or other
// function calls as arguments.
type FuncCall struct {
	Name string // Function name (e.g., "per_hour", "outliers")
	Args []Expr // Function arguments
}

func (*FuncCall) isExpr() {}

// DistributionQuery represents a Datadog distribution/histogram query with a value filter.
//
// Example: "count(v: v<10):trace.web.request{service:api}.as_count()"
//
// This syntax is used to count histogram values that meet a certain threshold.
// The comparison can be <, <=, >, or >=.
type DistributionQuery struct {
	Function   string       // Function name (typically "count")
	Comparator string       // Comparison operator: "<", "<=", ">", or ">="
	Threshold  float64      // Threshold value for comparison
	Query      *MetricQuery // The underlying metric query
}

func (*DistributionQuery) isExpr() {}

// StringLit represents a string literal expression.
//
// Example: "'DBSCAN'" in "outliers(..., 'DBSCAN', ...)"
type StringLit struct{ Value string }

func (*StringLit) isExpr() {}

// NumberLit represents a numeric literal expression.
//
// Example: "3" in "outliers(..., 'DBSCAN', 3)"
type NumberLit struct{ Value float64 }

func (*NumberLit) isExpr() {}

// IdentLit represents an identifier literal expression.
//
// Example: "zero" in "metric.fill(zero)"
type IdentLit struct{ Name string }

func (*IdentLit) isExpr() {}

// BinaryOp represents binary arithmetic operations: +, -, *, /
//
// Example: "a + b", "x * y", "(a - b) / c"
type BinaryOp struct {
	Op    string // Operator: "+", "-", "*", or "/"
	Left  Expr   // Left operand
	Right Expr   // Right operand
}

func (*BinaryOp) isExpr() {}

// UnaryOp represents unary operators: +expr, -expr
//
// Example: "-sum:metric{*}", "+100", "-func()"
type UnaryOp struct {
	Op   string // Operator: "+" or "-"
	Expr Expr   // Operand expression
}

func (*UnaryOp) isExpr() {}

// ExprList represents a comma-separated list of expressions at the top level.
//
// Example: "metric1{tag:value}, metric2{tag:value}"
type ExprList struct {
	Exprs []Expr // List of expressions
}

func (*ExprList) isExpr() {}

// Modifier represents a metric query modifier.
//
// Modifiers are chained after metric queries using dot notation.
// Examples:
//   - ".as_count()" - No arguments
//   - ".fill(zero)" - One argument
//   - ".rollup(avg, 20)" - Multiple arguments
type Modifier struct {
	Name string // Modifier name (e.g., "as_count", "fill", "rollup")
	Args []Expr // Modifier arguments
}

// TagExpr represents a tag filter expression used in metric query scopes.
//
// The following types implement TagExpr:
//   - TagTerm: A single tag key:value pair or variable
//   - TagAnd: Logical AND of multiple tag expressions
//   - TagOr: Logical OR of multiple tag expressions
//   - TagNot: Logical NOT of a tag expression
//   - TagIn: IN clause for matching multiple values
type TagExpr interface {
	isTagExpr()
}

// TagTerm represents a single tag filter term.
//
// A TagTerm can represent:
//   - A key:value pair: "env:prod"
//   - A bare key: "env" (matches any value)
//   - A variable: "$product" (Variable field contains "product" without '$')
//   - A negated term: "!env:prod" or "NOT env:prod"
//
// Only one of Key, Value, or Variable will be set for a given term.
type TagTerm struct {
	Negated  bool   // Whether this term is negated
	Key      string // Tag key (for key:value or bare key)
	Value    string // Tag value (for key:value pairs)
	Variable string // Variable name without leading '$' (e.g., "product" for "$product")
	Raw      string // Original source text (for debugging/reconstruction)
}

func (*TagTerm) isTagExpr() {}

// TagAnd represents a logical AND of multiple tag expressions.
//
// Example: "env:prod AND service:api" or "env:prod, service:api" (symbolic mode)
type TagAnd struct{ Items []TagExpr }

func (*TagAnd) isTagExpr() {}

// TagOr represents a logical OR of multiple tag expressions.
//
// Example: "env:prd OR env:shd"
type TagOr struct{ Items []TagExpr }

func (*TagOr) isTagExpr() {}

// TagNot represents a logical NOT of a tag expression.
//
// Example: "NOT env:prod" or "!env:prod"
type TagNot struct{ Item TagExpr }

func (*TagNot) isTagExpr() {}

// TagIn represents an IN clause for matching multiple tag values.
//
// Example: "env IN (prod, staging, dev)" or "env NOT IN (prod, staging)"
type TagIn struct {
	Negated bool     // Whether this is a NOT IN clause
	Key     string   // Tag key
	Values  []string // List of values to match
}

func (*TagIn) isTagExpr() {}

func strPtr(s string) *string { return &s }
