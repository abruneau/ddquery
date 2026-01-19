package ddquery

import (
	"reflect"
	"strings"
	"testing"
)

func TestParse_MetricQueries_Examples(t *testing.T) {
	cases := []string{
		`avg:airflow.job.end{$host} by {job_name,host}.as_count()`,
		`max:airflow.ti.finish{$host,$dag_id,$task_id,state:success} by {dag_id,task_id,state}.as_count()`,
		`avg:airflow.dag_processing.processes{*} by {host}.as_count()`,
		`per_hour(avg:airflow.job.end{$host} by {job_name,host}.as_count())`,
		`outliers(per_hour(avg:airflow.job.end{$host} by {job_name,host}.as_count()), 'DBSCAN', 3)`,
		`per_minute(per_second(avg:metric.name{$scope} by {tag}.as_rate()))`,
		`avg:metric.name{$scope} by {tag}.as_rate()`,
		`per_hour(avg:metric.name{$scope} by {tag}.as_rate())`,
		`avg:metric.name{$env, param:value}.fill(zero)`,
		`avg:metric.name{$env, param:value}.fill(zero).rollup(avg, 20)`,
		`metric.name{param:value} by {pod_name}`,
	}

	for _, q := range cases {
		if _, err := Parse(q); err != nil {
			t.Fatalf("Parse failed for %q: %v", q, err)
		}
	}
}

func TestParse_BooleanScopes(t *testing.T) {
	q := `sum:kubernetes.pods.running{(env:prd OR env:shd) AND $product}`
	ex, err := Parse(q)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	mq, ok := ex.(*MetricQuery)
	if !ok {
		t.Fatalf("expected MetricQuery, got %T", ex)
	}
	if mq.Aggregator == nil || *mq.Aggregator != "sum" {
		t.Fatalf("aggregator: %#v", mq.Aggregator)
	}
	if mq.Metric != "kubernetes.pods.running" {
		t.Fatalf("metric: %s", mq.Metric)
	}

	wantScope := &TagAnd{Items: []TagExpr{
		&TagOr{Items: []TagExpr{
			&TagTerm{Key: "env", Value: "prd", Raw: "env:prd"},
			&TagTerm{Key: "env", Value: "shd", Raw: "env:shd"},
		}},
		&TagTerm{Variable: "product", Raw: "$product"},
	}}

	if !reflect.DeepEqual(mq.Scope, wantScope) {
		t.Fatalf("scope mismatch\nwant: %#v\ngot:  %#v", wantScope, mq.Scope)
	}
}

func TestParse_BooleanScopes_WithNotAndGroupBy(t *testing.T) {
	q := `sum:kubernetes_state.pod.status_phase{(env:prd OR env:shd) AND NOT pod_phase:running AND NOT pod_phase:succeeded AND $product} by {pod_phase,kube_namespace}`
	ex, err := Parse(q)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	mq := ex.(*MetricQuery)

	if !reflect.DeepEqual(mq.GroupBy, []string{"pod_phase", "kube_namespace"}) {
		t.Fatalf("groupby: %#v", mq.GroupBy)
	}

	wantScope := &TagAnd{Items: []TagExpr{
		&TagOr{Items: []TagExpr{
			&TagTerm{Key: "env", Value: "prd", Raw: "env:prd"},
			&TagTerm{Key: "env", Value: "shd", Raw: "env:shd"},
		}},
		&TagNot{Item: &TagTerm{Key: "pod_phase", Value: "running", Raw: "pod_phase:running"}},
		&TagNot{Item: &TagTerm{Key: "pod_phase", Value: "succeeded", Raw: "pod_phase:succeeded"}},
		&TagTerm{Variable: "product", Raw: "$product"},
	}}

	if !reflect.DeepEqual(mq.Scope, wantScope) {
		t.Fatalf("scope mismatch\nwant: %#v\ngot:  %#v", wantScope, mq.Scope)
	}
}

func TestParse_BooleanScopes_Another(t *testing.T) {
	q := `sum:kubernetes_state.container.status_report.count.waiting{reason:crashloopbackoff AND (env:prd OR env:shd) AND $product} by {pod_name}`
	ex, err := Parse(q)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	mq := ex.(*MetricQuery)

	wantGB := []string{"pod_name"}
	if !reflect.DeepEqual(mq.GroupBy, wantGB) {
		t.Fatalf("groupby mismatch: %#v", mq.GroupBy)
	}

	wantScope := &TagAnd{Items: []TagExpr{
		&TagTerm{Key: "reason", Value: "crashloopbackoff", Raw: "reason:crashloopbackoff"},
		&TagOr{Items: []TagExpr{
			&TagTerm{Key: "env", Value: "prd", Raw: "env:prd"},
			&TagTerm{Key: "env", Value: "shd", Raw: "env:shd"},
		}},
		&TagTerm{Variable: "product", Raw: "$product"},
	}}

	if !reflect.DeepEqual(mq.Scope, wantScope) {
		t.Fatalf("scope mismatch\nwant: %#v\ngot:  %#v", wantScope, mq.Scope)
	}
}

func TestParse_Modifiers(t *testing.T) {
	q := `avg:metric.name{$env, param:value}.fill(zero).rollup(avg, 20)`
	ex, err := Parse(q)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	mq := ex.(*MetricQuery)

	if len(mq.Modifiers) != 2 {
		t.Fatalf("expected 2 modifiers, got %d", len(mq.Modifiers))
	}
	if mq.Modifiers[0].Name != "fill" || mq.Modifiers[1].Name != "rollup" {
		t.Fatalf("modifier names: %#v", mq.Modifiers)
	}
	if _, ok := mq.Modifiers[0].Args[0].(*IdentLit); !ok {
		t.Fatalf("fill arg type: %T", mq.Modifiers[0].Args[0])
	}
	if _, ok := mq.Modifiers[1].Args[1].(*NumberLit); !ok {
		t.Fatalf("rollup arg type: %T", mq.Modifiers[1].Args[1])
	}
}

func TestParse_NestedFunctions(t *testing.T) {
	q := `outliers(per_hour(avg:airflow.job.end{$host} by {job_name,host}.as_count()), 'DBSCAN', 3)`
	ex, err := Parse(q)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	fc, ok := ex.(*FuncCall)
	if !ok {
		t.Fatalf("expected FuncCall, got %T", ex)
	}
	if fc.Name != "outliers" || len(fc.Args) != 3 {
		t.Fatalf("func: %#v", fc)
	}
	fc2, ok := fc.Args[0].(*FuncCall)
	if !ok {
		t.Fatalf("expected FuncCall in arg0, got %T", fc.Args[0])
	}
	if fc2.Name != "per_hour" || len(fc2.Args) != 1 {
		t.Fatalf("func: %#v", fc2)
	}
	if _, ok := fc.Args[1].(*StringLit); !ok {
		t.Fatalf("expected StringLit in arg2, got %T", fc.Args[1])
	}
	if _, ok := fc.Args[2].(*NumberLit); !ok {
		t.Fatalf("expected NumberLit in arg3, got %T", fc.Args[2])
	}
}

// Test error cases
func TestParse_ErrorCases(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantErr     bool
		errContains string
	}{
		{"unclosed brace", "avg:metric{env:prod", true, ""}, // Will error, but message may vary
		{"unclosed paren in scope", "avg:metric{env:prod(", true, ""},
		{"unclosed paren in function", "func(arg", true, "expected ')'"},
		{"unexpected token after query", "avg:metric{env:prod} extra", true, "unexpected token"},
		{"empty input", "", true, "unexpected"},
		{"unclosed modifier paren", "metric.fill(zero", true, "expected ')'"},
		{"unclosed string", "metric{key:'unclosed", true, ""},            // May error or not depending on implementation
		{"unclosed IN list", "metric{key IN (val1,val2", true, ""},       // Will error, message may vary
		{"invalid value in IN list", "metric{key IN (val1,)}", true, ""}, // Will error somewhere
		{"unclosed paren in nested function", "func1(func2(arg", true, "expected ')'"},
		{"missing closing brace in boolean scope", "metric{key AND key2:val", true, ""},
		{"invalid token", "metric{@invalid}", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for %q, got nil", tt.input)
				} else if tt.errContains != "" {
					if errStr := err.Error(); !contains(errStr, tt.errContains) {
						t.Errorf("error %q should contain %q", errStr, tt.errContains)
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for %q: %v", tt.input, err)
				}
			}
		})
	}
}

// Test ParseError.Error() method (0% coverage)
func TestParseError_Error(t *testing.T) {
	err := &ParseError{
		Pos: 42,
		Msg: "test error message",
	}
	errStr := err.Error()
	if errStr == "" {
		t.Fatal("Error() returned empty string")
	}
	if !contains(errStr, "42") {
		t.Errorf("error message should contain position: %q", errStr)
	}
	if !contains(errStr, "test error message") {
		t.Errorf("error message should contain message: %q", errStr)
	}
}

// Test parseParenValueList (0% coverage) - IN clauses
func TestParse_TagIn(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantKey  string
		wantVals []string
		negated  bool
	}{
		{
			name:     "IN with single value",
			input:    "metric{key IN (value1)}",
			wantKey:  "key",
			wantVals: []string{"value1"},
			negated:  false,
		},
		{
			name:     "IN with multiple values",
			input:    "metric{key IN (val1, val2, val3)}",
			wantKey:  "key",
			wantVals: []string{"val1", "val2", "val3"},
			negated:  false,
		},
		{
			name:     "IN with string values",
			input:    "metric{key IN ('val1', 'val2')}",
			wantKey:  "key",
			wantVals: []string{"val1", "val2"},
			negated:  false,
		},
		{
			name:     "IN with number values",
			input:    "metric{key IN (1, 2, 3)}",
			wantKey:  "key",
			wantVals: []string{"1", "2", "3"},
			negated:  false,
		},
		{
			name:     "NOT IN",
			input:    "metric{key NOT IN (val1, val2)}",
			wantKey:  "key",
			wantVals: []string{"val1", "val2"},
			negated:  true,
		},
		{
			name:     "IN with empty list",
			input:    "metric{key IN ()}",
			wantKey:  "key",
			wantVals: []string{},
			negated:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}
			mq, ok := ex.(*MetricQuery)
			if !ok {
				t.Fatalf("expected MetricQuery, got %T", ex)
			}
			tagIn, ok := mq.Scope.(*TagIn)
			if !ok {
				t.Fatalf("expected TagIn, got %T", mq.Scope)
			}
			if tagIn.Key != tt.wantKey {
				t.Errorf("key: got %q, want %q", tagIn.Key, tt.wantKey)
			}
			if tagIn.Negated != tt.negated {
				t.Errorf("negated: got %v, want %v", tagIn.Negated, tt.negated)
			}
			if !reflect.DeepEqual(tagIn.Values, tt.wantVals) {
				t.Errorf("values: got %v, want %v", tagIn.Values, tt.wantVals)
			}
		})
	}
}

// Test parseTagNot more thoroughly (53.8% coverage)
func TestParse_TagNot(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantType string
	}{
		{
			name:     "NOT with keyword",
			input:    "metric{NOT key:value}",
			wantType: "TagNot",
		},
		{
			name:     "NOT with bang operator in boolean mode",
			input:    "metric{NOT !key:value}",
			wantType: "TagNot",
		},
		{
			name:     "double NOT",
			input:    "metric{NOT NOT key:value}",
			wantType: "TagNot",
		},
		{
			name:     "double NOT",
			input:    "metric{NOT NOT key:value}",
			wantType: "TagNot",
		},
		{
			name:     "NOT with variable",
			input:    "metric{NOT $var}",
			wantType: "TagNot",
		},
		{
			name:     "NOT with parenthesized expression",
			input:    "metric{NOT (key1:val1 OR key2:val2)}",
			wantType: "TagNot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}
			mq, ok := ex.(*MetricQuery)
			if !ok {
				t.Fatalf("expected MetricQuery, got %T", ex)
			}
			// Check that scope contains TagNot
			checkContainsTagNot(t, mq.Scope, tt.wantType)
		})
	}
}

func checkContainsTagNot(t *testing.T, expr TagExpr, wantType string) {
	t.Helper()
	if expr == nil {
		t.Error("scope is nil")
		return
	}
	switch v := expr.(type) {
	case *TagNot:
		// Found it
		return
	case *TagAnd:
		for _, item := range v.Items {
			checkContainsTagNot(t, item, wantType)
		}
	case *TagOr:
		for _, item := range v.Items {
			checkContainsTagNot(t, item, wantType)
		}
	case *TagTerm:
		// In symbolic mode, !key:value becomes a negated TagTerm, not TagNot
		if v.Negated {
			return // This is acceptable
		}
		t.Errorf("expected TagNot or negated TagTerm in scope, got non-negated TagTerm")
	default:
		t.Errorf("expected TagNot in scope, got %T", expr)
	}
}

// Test parseTagAtom more thoroughly (54.3% coverage)
func TestParse_TagAtom(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		validate func(t *testing.T, scope TagExpr)
	}{
		{
			name:  "bare term",
			input: "metric{key}",
			validate: func(t *testing.T, scope TagExpr) {
				term, ok := scope.(*TagTerm)
				if !ok {
					t.Fatalf("expected TagTerm, got %T", scope)
				}
				if term.Key != "key" {
					t.Errorf("key: got %q, want 'key'", term.Key)
				}
				if term.Value != "" {
					t.Errorf("value: got %q, want empty", term.Value)
				}
			},
		},
		{
			name:  "key:value",
			input: "metric{key:value}",
			validate: func(t *testing.T, scope TagExpr) {
				term, ok := scope.(*TagTerm)
				if !ok {
					t.Fatalf("expected TagTerm, got %T", scope)
				}
				if term.Key != "key" || term.Value != "value" {
					t.Errorf("got key=%q value=%q", term.Key, term.Value)
				}
			},
		},
		{
			name:  "variable",
			input: "metric{$var}",
			validate: func(t *testing.T, scope TagExpr) {
				term, ok := scope.(*TagTerm)
				if !ok {
					t.Fatalf("expected TagTerm, got %T", scope)
				}
				if term.Variable != "var" {
					t.Errorf("variable: got %q, want 'var'", term.Variable)
				}
			},
		},
		{
			name:  "key with string value",
			input: "metric{key:'string value'}",
			validate: func(t *testing.T, scope TagExpr) {
				term, ok := scope.(*TagTerm)
				if !ok {
					t.Fatalf("expected TagTerm, got %T", scope)
				}
				if term.Value != "string value" {
					t.Errorf("value: got %q", term.Value)
				}
			},
		},
		{
			name:  "key with number value",
			input: "metric{key:123}",
			validate: func(t *testing.T, scope TagExpr) {
				term, ok := scope.(*TagTerm)
				if !ok {
					t.Fatalf("expected TagTerm, got %T", scope)
				}
				if term.Value != "123" {
					t.Errorf("value: got %q, want '123'", term.Value)
				}
			},
		},
		{
			name:  "parenthesized expression in atom",
			input: "metric{(key1:val1 OR key2:val2)}",
			validate: func(t *testing.T, scope TagExpr) {
				_, ok := scope.(*TagOr)
				if !ok {
					t.Fatalf("expected TagOr, got %T", scope)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}
			mq, ok := ex.(*MetricQuery)
			if !ok {
				t.Fatalf("expected MetricQuery, got %T", ex)
			}
			tt.validate(t, mq.Scope)
		})
	}
}

// Test edge cases
func TestParse_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, ex Expr, err error)
	}{
		{
			name:  "empty scope",
			input: "metric.name{}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				mq, ok := ex.(*MetricQuery)
				if !ok {
					t.Fatalf("expected MetricQuery, got %T", ex)
				}
				if mq.Scope != nil {
					t.Errorf("expected nil scope, got %v", mq.Scope)
				}
			},
		},
		{
			name:  "empty groupby",
			input: "metric.name by {}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				mq, ok := ex.(*MetricQuery)
				if !ok {
					t.Fatalf("expected MetricQuery, got %T", ex)
				}
				if len(mq.GroupBy) != 0 {
					t.Errorf("expected empty groupby, got %v", mq.GroupBy)
				}
			},
		},
		{
			name:  "empty function args",
			input: "func()",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				fc, ok := ex.(*FuncCall)
				if !ok {
					t.Fatalf("expected FuncCall, got %T", ex)
				}
				if len(fc.Args) != 0 {
					t.Errorf("expected empty args, got %v", fc.Args)
				}
			},
		},
		{
			name:  "function with single arg",
			input: "func(arg1)",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				fc, ok := ex.(*FuncCall)
				if !ok {
					t.Fatalf("expected FuncCall, got %T", ex)
				}
				if len(fc.Args) != 1 {
					t.Errorf("expected 1 arg, got %d", len(fc.Args))
				}
			},
		},
		{
			name:  "negative number",
			input: "metric{count:-42}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				mq, ok := ex.(*MetricQuery)
				if !ok {
					t.Fatalf("expected MetricQuery, got %T", ex)
				}
				term, ok := mq.Scope.(*TagTerm)
				if !ok {
					t.Fatalf("expected TagTerm, got %T", mq.Scope)
				}
				if term.Value != "-42" {
					t.Errorf("value: got %q, want '-42'", term.Value)
				}
			},
		},
		{
			name:  "string literal in function",
			input: "func('string')",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				fc, ok := ex.(*FuncCall)
				if !ok {
					t.Fatalf("expected FuncCall, got %T", ex)
				}
				if _, ok := fc.Args[0].(*StringLit); !ok {
					t.Fatalf("expected StringLit, got %T", fc.Args[0])
				}
			},
		},
		{
			name:  "number literal in function",
			input: "func(42)",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				fc, ok := ex.(*FuncCall)
				if !ok {
					t.Fatalf("expected FuncCall, got %T", ex)
				}
				if _, ok := fc.Args[0].(*NumberLit); !ok {
					t.Fatalf("expected NumberLit, got %T", fc.Args[0])
				}
			},
		},
		{
			name:  "parenthesized expression",
			input: "(avg:metric.name)",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				_, ok := ex.(*MetricQuery)
				if !ok {
					t.Fatalf("expected MetricQuery, got %T", ex)
				}
			},
		},
		{
			name:  "symbolic scope with negation",
			input: "metric{!key:value}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				mq, ok := ex.(*MetricQuery)
				if !ok {
					t.Fatalf("expected MetricQuery, got %T", ex)
				}
				term, ok := mq.Scope.(*TagTerm)
				if !ok {
					t.Fatalf("expected TagTerm, got %T", mq.Scope)
				}
				if !term.Negated {
					t.Error("expected negated term")
				}
			},
		},
		{
			name:  "symbolic scope with comma",
			input: "metric{key1:val1, key2:val2}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				mq, ok := ex.(*MetricQuery)
				if !ok {
					t.Fatalf("expected MetricQuery, got %T", ex)
				}
				and, ok := mq.Scope.(*TagAnd)
				if !ok {
					t.Fatalf("expected TagAnd, got %T", mq.Scope)
				}
				if len(and.Items) != 2 {
					t.Errorf("expected 2 items, got %d", len(and.Items))
				}
			},
		},
		{
			name:  "multiple modifiers",
			input: "avg:metric.name{}.fill(zero).rollup(avg, 20).as_count()",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				mq, ok := ex.(*MetricQuery)
				if !ok {
					t.Fatalf("expected MetricQuery, got %T", ex)
				}
				if len(mq.Modifiers) != 3 {
					t.Errorf("expected 3 modifiers, got %d", len(mq.Modifiers))
				}
			},
		},
		{
			name:  "modifier with no args",
			input: "avg:metric.name{}.as_count()",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				mq, ok := ex.(*MetricQuery)
				if !ok {
					t.Fatalf("expected MetricQuery, got %T", ex)
				}
				if len(mq.Modifiers) != 1 {
					t.Fatalf("expected 1 modifier, got %d", len(mq.Modifiers))
				}
				if len(mq.Modifiers[0].Args) != 0 {
					t.Errorf("expected 0 args, got %d", len(mq.Modifiers[0].Args))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex, err := Parse(tt.input)
			tt.check(t, ex, err)
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// Benchmark tests
func BenchmarkParse_SimpleMetricQuery(b *testing.B) {
	query := "avg:metric.name{env:prod,service:api} by {host}.as_count()"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Parse(query)
	}
}

func BenchmarkParse_ComplexMetricQuery(b *testing.B) {
	query := "avg:airflow.job.end{$host} by {job_name,host}.as_count()"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Parse(query)
	}
}

func BenchmarkParse_BooleanScope(b *testing.B) {
	query := "sum:kubernetes.pods.running{(env:prd OR env:shd) AND $product} by {pod_phase,kube_namespace}"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Parse(query)
	}
}

func BenchmarkParse_NestedFunctions(b *testing.B) {
	query := "outliers(per_hour(avg:airflow.job.end{$host} by {job_name,host}.as_count()), 'DBSCAN', 3)"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Parse(query)
	}
}

func BenchmarkParse_MultipleModifiers(b *testing.B) {
	query := "avg:metric.name{$env, param:value}.fill(zero).rollup(avg, 20).as_count()"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Parse(query)
	}
}

func BenchmarkParse_TagIn(b *testing.B) {
	query := "metric{key IN (val1, val2, val3, val4, val5)}"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Parse(query)
	}
}

func BenchmarkParse_DeepNesting(b *testing.B) {
	query := "per_minute(per_second(avg:metric.name{$scope} by {tag}.as_rate()))"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Parse(query)
	}
}

func BenchmarkParse_SimpleQuery(b *testing.B) {
	query := "metric.name{param:value}"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Parse(query)
	}
}
