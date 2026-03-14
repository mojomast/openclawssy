package eval

import (
	"context"
	"time"
)

type SetupFunc func(context.Context) error

type RunFunc func(context.Context) TestResult

type CleanupFunc func(context.Context) error

type Suite struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	TestCases   []TestCase `json:"test_cases"`
}

type TestCase struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Setup       SetupFunc   `json:"-"`
	Run         RunFunc     `json:"-"`
	Cleanup     CleanupFunc `json:"-"`
}

type TestResult struct {
	Passed     bool   `json:"passed"`
	Actual     string `json:"actual"`
	Expected   string `json:"expected"`
	DurationMS int    `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type CaseResult struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Result      TestResult `json:"result"`
}

type SuiteRun struct {
	ID          int64        `json:"id,omitempty"`
	Suite       string       `json:"suite"`
	Description string       `json:"description,omitempty"`
	Timestamp   time.Time    `json:"timestamp"`
	Results     []CaseResult `json:"results"`
	Metrics     Metrics      `json:"metrics"`
}

type Metrics struct {
	CompletionRate            float64 `json:"completion_rate"`
	ToolMisuseRate            float64 `json:"tool_misuse_rate"`
	DelegationPrecision       float64 `json:"delegation_precision"`
	UnnecessaryDelegationRate float64 `json:"unnecessary_delegation_rate"`
	TokenCost                 int     `json:"token_cost"`
	TimeToCompletion          int     `json:"time_to_completion"`
}
