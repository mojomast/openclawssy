package eval

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	toolPattern       = regexp.MustCompile(`(?i)tool\s*:\s*([a-z0-9._/-]+)`)
	delegationPattern = regexp.MustCompile(`(?i)delegate(?:d|ion)?\s*:\s*(yes|no|true|false|1|0)`)
	tokenPattern      = regexp.MustCompile(`(?i)tokens?\s*[:=]\s*(\d+)`)
)

func ComputeMetrics(results []TestResult) Metrics {
	metrics := Metrics{}
	total := len(results)
	if total == 0 {
		return metrics
	}

	passedCount := 0
	toolCases := 0
	toolMisuseCount := 0
	predictedDelegations := 0
	truePositiveDelegations := 0
	expectedNegativeDelegations := 0
	unnecessaryDelegations := 0

	for _, result := range results {
		if result.Passed {
			passedCount++
		}
		if result.DurationMS > 0 {
			metrics.TimeToCompletion += result.DurationMS
		}
		metrics.TokenCost += parseTokenCost(result)

		expectedTool, hasExpectedTool := parseTool(result.Expected)
		actualTool, hasActualTool := parseTool(result.Actual)
		if hasExpectedTool {
			toolCases++
			if !hasActualTool || actualTool != expectedTool {
				toolMisuseCount++
			}
		}

		expectedDelegation, hasExpectedDelegation := parseDelegation(result.Expected)
		actualDelegation, hasActualDelegation := parseDelegation(result.Actual)
		if hasExpectedDelegation {
			if hasActualDelegation && actualDelegation {
				predictedDelegations++
				if expectedDelegation {
					truePositiveDelegations++
				}
			}
			if !expectedDelegation {
				expectedNegativeDelegations++
				if hasActualDelegation && actualDelegation {
					unnecessaryDelegations++
				}
			}
		}
	}

	metrics.CompletionRate = float64(passedCount) / float64(total)
	if toolCases > 0 {
		metrics.ToolMisuseRate = float64(toolMisuseCount) / float64(toolCases)
	}
	if predictedDelegations > 0 {
		metrics.DelegationPrecision = float64(truePositiveDelegations) / float64(predictedDelegations)
	}
	if expectedNegativeDelegations > 0 {
		metrics.UnnecessaryDelegationRate = float64(unnecessaryDelegations) / float64(expectedNegativeDelegations)
	}

	return metrics
}

func parseTool(raw string) (string, bool) {
	matches := toolPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if len(matches) < 2 {
		return "", false
	}
	tool := strings.ToLower(strings.TrimSpace(matches[1]))
	if tool == "" {
		return "", false
	}
	return tool, true
}

func parseDelegation(raw string) (bool, bool) {
	matches := delegationPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if len(matches) < 2 {
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(matches[1])) {
	case "yes", "true", "1":
		return true, true
	case "no", "false", "0":
		return false, true
	default:
		return false, false
	}
}

func parseTokenCost(result TestResult) int {
	for _, candidate := range []string{result.Actual, result.Expected, result.Error} {
		matches := tokenPattern.FindStringSubmatch(candidate)
		if len(matches) < 2 {
			continue
		}
		value, err := strconv.Atoi(matches[1])
		if err != nil || value < 0 {
			continue
		}
		return value
	}
	return 0
}
