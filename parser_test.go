package ddquery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func loadMetricQueries(t *testing.T) []string {
	// Get the path to testdata/metric_queries.json relative to the project root
	// This works by finding the project root (where go.mod is) and then navigating to testdata
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	// Try to find the project root by looking for go.mod
	projectRoot := wd
	for {
		if _, err := os.Stat(filepath.Join(projectRoot, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(projectRoot)
		if parent == projectRoot {
			t.Fatalf("Could not find project root (go.mod)")
		}
		projectRoot = parent
	}

	jsonPath := filepath.Join(projectRoot, "testdata", "metric_queries.json")
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("Failed to read metric queries JSON file at %s: %v", jsonPath, err)
	}

	var cases []string
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("Failed to parse metric queries JSON: %v", err)
	}

	return cases
}

func TestParse_MetricQueries_Examples(t *testing.T) {
	cases := loadMetricQueries(t)

	if len(cases) == 0 {
		t.Fatal("No metric queries found in test data")
	}

	t.Logf("Testing %d metric queries from Datadog dashboards", len(cases))

	var failures []string
	successCount := 0

	for _, q := range cases {
		if _, err := Parse(q); err != nil {
			failures = append(failures, q)
		} else {
			successCount++
		}
	}

	t.Logf("Successfully parsed %d/%d queries", successCount, len(cases))

	if len(failures) > 0 {
		t.Logf("Failed to parse %d queries (these may contain unsupported syntax):", len(failures))
		// Only log first 10 failures to avoid cluttering output
		maxLog := min(len(failures), 10)
		for i := 0; i < maxLog; i++ {
			t.Logf("  - %q", failures[i])
		}
		if len(failures) > maxLog {
			t.Logf("  ... and %d more (see test output for full list)", len(failures)-maxLog)
		}
		// Uncomment the line below if you want the test to fail on any parse error:
		t.Fatalf("%d queries failed to parse", len(failures))
	}
}

func loadMonitorQueries(t *testing.T) []string {
	// Get the path to testdata/monitor_queries.json relative to the project root
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	// Try to find the project root by looking for go.mod
	projectRoot := wd
	for {
		if _, err := os.Stat(filepath.Join(projectRoot, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(projectRoot)
		if parent == projectRoot {
			t.Fatalf("Could not find project root (go.mod)")
		}
		projectRoot = parent
	}

	jsonPath := filepath.Join(projectRoot, "testdata", "monitor_queries.json")
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("Failed to read monitor queries JSON file at %s: %v", jsonPath, err)
	}

	var cases []string
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("Failed to parse monitor queries JSON: %v", err)
	}

	return cases
}

func TestParse_MonitorQueries_Examples(t *testing.T) {
	cases := loadMonitorQueries(t)

	if len(cases) == 0 {
		t.Fatal("No monitor queries found in test data")
	}

	t.Logf("Testing %d monitor queries from Datadog monitors", len(cases))

	var failures []string
	successCount := 0

	for _, q := range cases {
		if _, err := Parse(q); err != nil {
			failures = append(failures, q)
		} else {
			successCount++
		}
	}

	t.Logf("Successfully parsed %d/%d queries", successCount, len(cases))

	if len(failures) > 0 {
		t.Logf("Failed to parse %d queries:", len(failures))
		// Log first 20 failures to see the patterns
		maxLog := min(len(failures), 20)
		for i := 0; i < maxLog; i++ {
			t.Logf("  - %q", failures[i])
		}
		if len(failures) > maxLog {
			t.Logf("  ... and %d more", len(failures)-maxLog)
		}
		t.Fatalf("%d queries failed to parse", len(failures))
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

// Test patterns that were previously failing
func TestParse_FixedPatterns(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, ex Expr, err error)
	}{
		{
			name:  "value with embedded colon (GCP database ID)",
			input: "avg:gcp.cloudsql.database.disk.bytes_used{database_id:blv-shared-services:myconsole-14dc2a48}",
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
				if term.Value != "blv-shared-services:myconsole-14dc2a48" {
					t.Errorf("value: got %q, want %q", term.Value, "blv-shared-services:myconsole-14dc2a48")
				}
			},
		},
		{
			name:  "value starting with dot",
			input: "avg:elastic_cloud.index.docs.count{$node_name,$deployment_name ,!index_name:.internal*} by {index_name}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				_, ok := ex.(*MetricQuery)
				if !ok {
					t.Fatalf("expected MetricQuery, got %T", ex)
				}
				// Should parse successfully
			},
		},
		{
			name:  "double-quoted string in scope",
			input: `sum:confluent_cloud.kafka.consumer_lag_offsets{$env AND topic:"datapipeline-r2a-common*"} by {topic}`,
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				_, ok := ex.(*MetricQuery)
				if !ok {
					t.Fatalf("expected MetricQuery, got %T", ex)
				}
				// Should parse successfully
			},
		},
		{
			name:  "empty value after colon",
			input: "sum:kubernetes.liveness_probe.failure.total{env:prd,cluster_name:zeus,service:}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				_, ok := ex.(*MetricQuery)
				if !ok {
					t.Fatalf("expected MetricQuery, got %T", ex)
				}
				// Should parse successfully with empty value
			},
		},
		{
			name:  "lowercase or with comma (mixed boolean/symbolic)",
			input: "sum:openai.tokens.total{openai.request.model:text-ada-001 or openai.request.model:ada,$model}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				_, ok := ex.(*MetricQuery)
				if !ok {
					t.Fatalf("expected MetricQuery, got %T", ex)
				}
				// Should parse successfully
			},
		},
		{
			name:  "variable with value (dollar-sign key)",
			input: "sum:traefik_mesh.router.requests.count{$host,$service,$routercode:3*} by {code,router,service}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				_, ok := ex.(*MetricQuery)
				if !ok {
					t.Fatalf("expected MetricQuery, got %T", ex)
				}
				// Should parse successfully
			},
		},
		{
			name:  "distribution query with less than",
			input: "count(v: v<10):trace.web.request{service:api}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				dq, ok := ex.(*DistributionQuery)
				if !ok {
					t.Fatalf("expected DistributionQuery, got %T", ex)
				}
				if dq.Comparator != "<" {
					t.Errorf("comparator: got %q, want %q", dq.Comparator, "<")
				}
				if dq.Threshold != 10 {
					t.Errorf("threshold: got %f, want %f", dq.Threshold, 10.0)
				}
			},
		},
		{
			name:  "distribution query with greater than or equal",
			input: "count(v: v>=0):data_streams.latency{direction:in}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				dq, ok := ex.(*DistributionQuery)
				if !ok {
					t.Fatalf("expected DistributionQuery, got %T", ex)
				}
				if dq.Comparator != ">=" {
					t.Errorf("comparator: got %q, want %q", dq.Comparator, ">=")
				}
				if dq.Threshold != 0 {
					t.Errorf("threshold: got %f, want %f", dq.Threshold, 0.0)
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

// Test arithmetic operations
func TestParse_Arithmetic(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, ex Expr, err error)
	}{
		{
			name:  "simple addition",
			input: "sum:metric1{*} + sum:metric2{*}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				op, ok := ex.(*BinaryOp)
				if !ok {
					t.Fatalf("expected BinaryOp, got %T", ex)
				}
				if op.Op != "+" {
					t.Errorf("op: got %q, want '+'", op.Op)
				}
			},
		},
		{
			name:  "simple subtraction",
			input: "sum:metric1{*} - sum:metric2{*}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				op, ok := ex.(*BinaryOp)
				if !ok {
					t.Fatalf("expected BinaryOp, got %T", ex)
				}
				if op.Op != "-" {
					t.Errorf("op: got %q, want '-'", op.Op)
				}
			},
		},
		{
			name:  "simple multiplication",
			input: "sum:metric1{*} * 100",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				op, ok := ex.(*BinaryOp)
				if !ok {
					t.Fatalf("expected BinaryOp, got %T", ex)
				}
				if op.Op != "*" {
					t.Errorf("op: got %q, want '*'", op.Op)
				}
			},
		},
		{
			name:  "simple division",
			input: "sum:metric1{*} / sum:metric2{*}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				op, ok := ex.(*BinaryOp)
				if !ok {
					t.Fatalf("expected BinaryOp, got %T", ex)
				}
				if op.Op != "/" {
					t.Errorf("op: got %q, want '/'", op.Op)
				}
			},
		},
		{
			name:  "precedence: multiplication before addition",
			input: "a + b * c",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				// Should parse as: a + (b * c)
				op, ok := ex.(*BinaryOp)
				if !ok || op.Op != "+" {
					t.Fatalf("expected BinaryOp with '+', got %T", ex)
				}
				right, ok := op.Right.(*BinaryOp)
				if !ok || right.Op != "*" {
					t.Fatalf("expected right operand to be BinaryOp with '*', got %T", op.Right)
				}
			},
		},
		{
			name:  "precedence: division before subtraction",
			input: "a - b / c",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				// Should parse as: a - (b / c)
				op, ok := ex.(*BinaryOp)
				if !ok || op.Op != "-" {
					t.Fatalf("expected BinaryOp with '-', got %T", ex)
				}
				right, ok := op.Right.(*BinaryOp)
				if !ok || right.Op != "/" {
					t.Fatalf("expected right operand to be BinaryOp with '/', got %T", op.Right)
				}
			},
		},
		{
			name:  "parentheses override precedence",
			input: "(a + b) * c",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				// Should parse as: (a + b) * c
				op, ok := ex.(*BinaryOp)
				if !ok || op.Op != "*" {
					t.Fatalf("expected BinaryOp with '*', got %T", ex)
				}
				left, ok := op.Left.(*BinaryOp)
				if !ok || left.Op != "+" {
					t.Fatalf("expected left operand to be BinaryOp with '+', got %T", op.Left)
				}
			},
		},
		{
			name:  "left associativity: subtraction",
			input: "a - b - c",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				// Should parse as: ((a - b) - c)
				op, ok := ex.(*BinaryOp)
				if !ok || op.Op != "-" {
					t.Fatalf("expected BinaryOp with '-', got %T", ex)
				}
				left, ok := op.Left.(*BinaryOp)
				if !ok || left.Op != "-" {
					t.Fatalf("expected left operand to be BinaryOp with '-', got %T", op.Left)
				}
			},
		},
		{
			name:  "left associativity: division",
			input: "a / b / c",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				// Should parse as: ((a / b) / c)
				op, ok := ex.(*BinaryOp)
				if !ok || op.Op != "/" {
					t.Fatalf("expected BinaryOp with '/', got %T", ex)
				}
				left, ok := op.Left.(*BinaryOp)
				if !ok || left.Op != "/" {
					t.Fatalf("expected left operand to be BinaryOp with '/', got %T", op.Left)
				}
			},
		},
		{
			name:  "complex nested arithmetic",
			input: "( ( sum:metric1{*} - sum:metric2{*} ) / sum:metric3{*} ) * 100",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				// Should parse successfully
				_, ok := ex.(*BinaryOp)
				if !ok {
					t.Fatalf("expected BinaryOp, got %T", ex)
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

// Test unary operators
func TestParse_UnaryOperators(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, ex Expr, err error)
	}{
		{
			name:  "unary minus on metric query",
			input: "-sum:metric{*}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				unary, ok := ex.(*UnaryOp)
				if !ok {
					t.Fatalf("expected UnaryOp, got %T", ex)
				}
				if unary.Op != "-" {
					t.Errorf("op: got %q, want '-'", unary.Op)
				}
				_, ok = unary.Expr.(*MetricQuery)
				if !ok {
					t.Fatalf("expected MetricQuery as operand, got %T", unary.Expr)
				}
			},
		},
		{
			name:  "unary minus on function call",
			input: "-func()",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				unary, ok := ex.(*UnaryOp)
				if !ok {
					t.Fatalf("expected UnaryOp, got %T", ex)
				}
				if unary.Op != "-" {
					t.Errorf("op: got %q, want '-'", unary.Op)
				}
			},
		},
		{
			name:  "unary plus",
			input: "+100",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				unary, ok := ex.(*UnaryOp)
				if !ok {
					t.Fatalf("expected UnaryOp, got %T", ex)
				}
				if unary.Op != "+" {
					t.Errorf("op: got %q, want '+'", unary.Op)
				}
			},
		},
		{
			name:  "double unary minus",
			input: "--x",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				unary, ok := ex.(*UnaryOp)
				if !ok {
					t.Fatalf("expected UnaryOp, got %T", ex)
				}
				if unary.Op != "-" {
					t.Errorf("outer op: got %q, want '-'", unary.Op)
				}
				inner, ok := unary.Expr.(*UnaryOp)
				if !ok {
					t.Fatalf("expected inner UnaryOp, got %T", unary.Expr)
				}
				if inner.Op != "-" {
					t.Errorf("inner op: got %q, want '-'", inner.Op)
				}
			},
		},
		{
			name:  "unary minus with metric query and modifiers",
			input: "-sum:nginx.ssl.handshakes_failed_count{*} by {host}.as_count()",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				unary, ok := ex.(*UnaryOp)
				if !ok {
					t.Fatalf("expected UnaryOp, got %T", ex)
				}
				if unary.Op != "-" {
					t.Errorf("op: got %q, want '-'", unary.Op)
				}
			},
		},
		{
			name:  "zero minus expression",
			input: "0 - per_second(avg:metric{*})",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				op, ok := ex.(*BinaryOp)
				if !ok || op.Op != "-" {
					t.Fatalf("expected BinaryOp with '-', got %T", ex)
				}
				left, ok := op.Left.(*NumberLit)
				if !ok || left.Value != 0 {
					t.Fatalf("expected NumberLit(0) as left operand, got %T", op.Left)
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

// Test numeric literals as standalone expressions
func TestParse_NumericLiterals(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, ex Expr, err error)
	}{
		{
			name:  "standalone number",
			input: "100",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				num, ok := ex.(*NumberLit)
				if !ok {
					t.Fatalf("expected NumberLit, got %T", ex)
				}
				if num.Value != 100 {
					t.Errorf("value: got %f, want 100", num.Value)
				}
			},
		},
		{
			name:  "number in multiplication",
			input: "100 * sum:metric{*}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				op, ok := ex.(*BinaryOp)
				if !ok || op.Op != "*" {
					t.Fatalf("expected BinaryOp with '*', got %T", ex)
				}
				left, ok := op.Left.(*NumberLit)
				if !ok || left.Value != 100 {
					t.Fatalf("expected NumberLit(100) as left operand, got %T", op.Left)
				}
			},
		},
		{
			name:  "zero in subtraction",
			input: "0 - sum:metric{*}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				op, ok := ex.(*BinaryOp)
				if !ok || op.Op != "-" {
					t.Fatalf("expected BinaryOp with '-', got %T", ex)
				}
				left, ok := op.Left.(*NumberLit)
				if !ok || left.Value != 0 {
					t.Fatalf("expected NumberLit(0) as left operand, got %T", op.Left)
				}
			},
		},
		{
			name:  "large number",
			input: "1000 * sum:metric{*}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				op, ok := ex.(*BinaryOp)
				if !ok || op.Op != "*" {
					t.Fatalf("expected BinaryOp with '*', got %T", ex)
				}
				left, ok := op.Left.(*NumberLit)
				if !ok || left.Value != 1000 {
					t.Fatalf("expected NumberLit(1000) as left operand, got %T", op.Left)
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

// Test comma-separated expression lists
func TestParse_CommaSeparatedLists(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, ex Expr, err error)
	}{
		{
			name:  "two metric queries",
			input: "avg:metric1{tag:value}, avg:metric2{tag:value}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				list, ok := ex.(*ExprList)
				if !ok {
					t.Fatalf("expected ExprList, got %T", ex)
				}
				if len(list.Exprs) != 2 {
					t.Errorf("expected 2 expressions, got %d", len(list.Exprs))
				}
				for i, expr := range list.Exprs {
					_, ok := expr.(*MetricQuery)
					if !ok {
						t.Errorf("expr[%d]: expected MetricQuery, got %T", i, expr)
					}
				}
			},
		},
		{
			name:  "three expressions",
			input: "expr1, expr2, expr3",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				list, ok := ex.(*ExprList)
				if !ok {
					t.Fatalf("expected ExprList, got %T", ex)
				}
				if len(list.Exprs) != 3 {
					t.Errorf("expected 3 expressions, got %d", len(list.Exprs))
				}
			},
		},
		{
			name:  "real-world example from test data",
			input: "avg:aerospike.namespace.tps.read{$host,$namespace}, avg:aerospike.namespace.tps.write{$host,$namespace}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				list, ok := ex.(*ExprList)
				if !ok {
					t.Fatalf("expected ExprList, got %T", ex)
				}
				if len(list.Exprs) != 2 {
					t.Errorf("expected 2 expressions, got %d", len(list.Exprs))
				}
			},
		},
		{
			name:  "expressions with arithmetic",
			input: "a + b, c * d",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				list, ok := ex.(*ExprList)
				if !ok {
					t.Fatalf("expected ExprList, got %T", ex)
				}
				if len(list.Exprs) != 2 {
					t.Errorf("expected 2 expressions, got %d", len(list.Exprs))
				}
				_, ok = list.Exprs[0].(*BinaryOp)
				if !ok {
					t.Errorf("expr[0]: expected BinaryOp, got %T", list.Exprs[0])
				}
				_, ok = list.Exprs[1].(*BinaryOp)
				if !ok {
					t.Errorf("expr[1]: expected BinaryOp, got %T", list.Exprs[1])
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

// Test edge cases for operator vs identifier ambiguity
func TestParse_OperatorAmbiguity(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, ex Expr, err error)
	}{
		{
			name:  "metric name with dash (no spaces)",
			input: "metric-name{tag:value}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				mq, ok := ex.(*MetricQuery)
				if !ok {
					t.Fatalf("expected MetricQuery, got %T", ex)
				}
				if mq.Metric != "metric-name" {
					t.Errorf("metric: got %q, want 'metric-name'", mq.Metric)
				}
			},
		},
		{
			name:  "metric minus name (with spaces)",
			input: "metric - name",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				op, ok := ex.(*BinaryOp)
				if !ok || op.Op != "-" {
					t.Fatalf("expected BinaryOp with '-', got %T", ex)
				}
			},
		},
		{
			name:  "metric name with slash (no spaces)",
			input: "http/status{tag:value}",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				mq, ok := ex.(*MetricQuery)
				if !ok {
					t.Fatalf("expected MetricQuery, got %T", ex)
				}
				if mq.Metric != "http/status" {
					t.Errorf("metric: got %q, want 'http/status'", mq.Metric)
				}
			},
		},
		{
			name:  "a divided by b (with spaces)",
			input: "a / b",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				op, ok := ex.(*BinaryOp)
				if !ok || op.Op != "/" {
					t.Fatalf("expected BinaryOp with '/', got %T", ex)
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

// Test real-world complex examples from test data
func TestParse_RealWorldExamples(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, ex Expr, err error)
	}{
		{
			name:  "complex nested arithmetic with parentheses",
			input: "( ( sum:zookeeper.max_file_descriptor_count{*} - sum:zookeeper.open_file_descriptor_count{*} ) / sum:zookeeper.max_file_descriptor_count{*} ) * 100",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				// Should parse successfully
				_, ok := ex.(*BinaryOp)
				if !ok {
					t.Fatalf("expected BinaryOp, got %T", ex)
				}
			},
		},
		{
			name:  "unary minus on metric query",
			input: "-sum:nginx.ssl.handshakes_failed_count{*} by {host}.as_count()",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				unary, ok := ex.(*UnaryOp)
				if !ok {
					t.Fatalf("expected UnaryOp, got %T", ex)
				}
				if unary.Op != "-" {
					t.Errorf("op: got %q, want '-'", unary.Op)
				}
			},
		},
		{
			name:  "zero minus function call",
			input: "0 - per_second(avg:couchdb.couchdb.database_writes{$couchdb,$scope})",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				op, ok := ex.(*BinaryOp)
				if !ok || op.Op != "-" {
					t.Fatalf("expected BinaryOp with '-', got %T", ex)
				}
			},
		},
		{
			name:  "percentage calculation",
			input: "100 * ( avg:varnish.cache_hit{$scope} / ( avg:varnish.cache_hit{$scope} + avg:varnish.cache_miss{$scope} ) )",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				// Should parse successfully
				_, ok := ex.(*BinaryOp)
				if !ok {
					t.Fatalf("expected BinaryOp, got %T", ex)
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

// Test operators without spaces (compact expressions)
func TestParse_CompactExpressions(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, ex Expr, err error)
	}{
		{
			name:  "parenthesized expression times number",
			input: "(avg:metric{*})*100",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				op, ok := ex.(*BinaryOp)
				if !ok || op.Op != "*" {
					t.Fatalf("expected BinaryOp with '*', got %T", ex)
				}
			},
		},
		{
			name:  "parenthesized expression divided by number",
			input: "(avg:metric{*})/100",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				op, ok := ex.(*BinaryOp)
				if !ok || op.Op != "/" {
					t.Fatalf("expected BinaryOp with '/', got %T", ex)
				}
			},
		},
		{
			name:  "parenthesized expression minus expression",
			input: "(a)-b",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				op, ok := ex.(*BinaryOp)
				if !ok || op.Op != "-" {
					t.Fatalf("expected BinaryOp with '-', got %T", ex)
				}
			},
		},
		{
			name:  "complex percentage calculation",
			input: "(avg:metric1{*}-avg:metric2{*})/avg:metric1{*}*100",
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				// Should parse successfully
				_, ok := ex.(*BinaryOp)
				if !ok {
					t.Fatalf("expected BinaryOp, got %T", ex)
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

// Test * in boolean scope expressions
func TestParse_StarInBooleanScope(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, ex Expr, err error)
	}{
		{
			name:  "star AND key:value",
			input: "sum:metric{* AND key:value}",
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
					t.Fatalf("expected TagAnd scope, got %T", mq.Scope)
				}
				if len(and.Items) != 2 {
					t.Errorf("expected 2 items in AND, got %d", len(and.Items))
				}
			},
		},
		{
			name:  "star AND key IN list",
			input: "sum:metric{* AND key IN (val1,val2)}",
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
					t.Fatalf("expected TagAnd scope, got %T", mq.Scope)
				}
				if len(and.Items) != 2 {
					t.Errorf("expected 2 items in AND, got %d", len(and.Items))
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

// Test the last two failing queries
func TestParse_LastFailingQueries(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, ex Expr, err error)
	}{
		{
			name:  "logs query with escaped quotes",
			input: `logs("source:klaviyo service:ecommerce-events @metric_name:\"Refunded Order\"").index("*").rollup("count").last("1h") > 30`,
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Logf("Parse error: %v", err)
					t.Fatalf("failed to parse logs query with escaped quotes")
				}
			},
		},
		{
			name:  "sum with group-by syntax",
			input: `max(last_5m):sum(max:sqlserver.ao.replica_status{replica_role:primary} by {replica_server_name,availability_group_name}.rollup(max, 60), { availability_group_name }) > 1`,
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Logf("Parse error: %v", err)
					t.Fatalf("failed to parse sum with group-by syntax")
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

// Test specific failing patterns from monitor queries
func TestParse_MonitorQueryPatterns(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, ex Expr, err error)
	}{
		{
			name:  "service check query with method chaining",
			input: `"consul.check".over("*").by("host").last(3).count_by_status()`,
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Logf("Parse error: %v", err)
					t.Fatalf("failed to parse service check query")
				}
				// Should parse as a MethodCall chain
				_, ok := ex.(*MethodCall)
				if !ok {
					t.Fatalf("expected MethodCall, got %T", ex)
				}
			},
		},
		{
			name:  "change function with two timeframes",
			input: `change(avg(last_5m),last_1d):avg:metric{*} == 0`,
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Logf("Parse error: %v", err)
					t.Fatalf("failed to parse change monitor query")
				}
				// Should parse as MonitorQuery
				_, ok := ex.(*MonitorQuery)
				if !ok {
					t.Fatalf("expected MonitorQuery, got %T", ex)
				}
			},
		},
		{
			name:  "events function with method chaining",
			input: `events("source:windows_crash_detection").rollup("count").by("host").last("10m") > 0`,
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Logf("Parse error: %v", err)
					t.Fatalf("failed to parse events query")
				}
				// Should parse as MonitorQuery with MethodCall as query
				mq, ok := ex.(*MonitorQuery)
				if !ok {
					t.Fatalf("expected MonitorQuery, got %T", ex)
				}
				// The query part should be a MethodCall chain
				_, ok = mq.Query.(*MethodCall)
				if !ok {
					t.Fatalf("expected MethodCall in query, got %T", mq.Query)
				}
			},
		},
		{
			name:  "formula function with method chaining",
			input: `formula("(query - query1) / query").last("5m") > 0.1`,
			check: func(t *testing.T, ex Expr, err error) {
				if err != nil {
					t.Logf("Parse error: %v", err)
					t.Fatalf("failed to parse formula query")
				}
				// Should parse as MonitorQuery with MethodCall as query
				mq, ok := ex.(*MonitorQuery)
				if !ok {
					t.Fatalf("expected MonitorQuery, got %T", ex)
				}
				// The query part should be a MethodCall chain
				_, ok = mq.Query.(*MethodCall)
				if !ok {
					t.Fatalf("expected MethodCall in query, got %T", mq.Query)
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
