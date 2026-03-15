package eval

import (
	"math"
	"testing"
)

func TestComputeMetricsFromResults(t *testing.T) {
	t.Parallel()

	results := []TestResult{
		{
			Passed:     true,
			Expected:   "tool:fs.read",
			Actual:     "tool:fs.write tokens=10",
			DurationMS: 100,
		},
		{
			Passed:     false,
			Expected:   "delegate:no",
			Actual:     "delegate:yes tokens=15",
			DurationMS: 200,
		},
		{
			Passed:     true,
			Expected:   "delegate:yes",
			Actual:     "delegate:yes tokens=20",
			DurationMS: 300,
		},
	}

	metrics := ComputeMetrics(results)

	if diff := math.Abs(metrics.CompletionRate - (2.0 / 3.0)); diff > 1e-9 {
		t.Fatalf("completion rate = %f, want %f", metrics.CompletionRate, 2.0/3.0)
	}
	if diff := math.Abs(metrics.ToolMisuseRate - 1.0); diff > 1e-9 {
		t.Fatalf("tool misuse rate = %f, want %f", metrics.ToolMisuseRate, 1.0)
	}
	if diff := math.Abs(metrics.DelegationPrecision - 0.5); diff > 1e-9 {
		t.Fatalf("delegation precision = %f, want %f", metrics.DelegationPrecision, 0.5)
	}
	if diff := math.Abs(metrics.UnnecessaryDelegationRate - 1.0); diff > 1e-9 {
		t.Fatalf("unnecessary delegation rate = %f, want %f", metrics.UnnecessaryDelegationRate, 1.0)
	}
	if metrics.TokenCost != 45 {
		t.Fatalf("token cost = %d, want %d", metrics.TokenCost, 45)
	}
	if metrics.TimeToCompletion != 600 {
		t.Fatalf("time to completion = %d, want %d", metrics.TimeToCompletion, 600)
	}
}
