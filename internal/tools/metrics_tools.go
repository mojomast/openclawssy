package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	httpchannel "openclawssy/internal/channels/http"
)

const (
	defaultMetricsRunLimit = 200
	maxMetricsRunLimit     = 1000
)

func registerMetricsTools(reg *Registry, runsPath string) error {
	return reg.Register(ToolSpec{
		Name:        "metrics.get",
		Description: "Aggregate runtime metrics from runs",
		ArgTypes: map[string]ArgType{
			"agent_id": ArgTypeString,
			"status":   ArgTypeString,
			"limit":    ArgTypeNumber,
			"offset":   ArgTypeNumber,
		},
	}, metricsGet(runsPath))
}

func metricsGet(runsPath string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		store, err := openRunStore(req.Workspace, runsPath)
		if err != nil {
			return nil, err
		}
		runs, err := store.List(nil)
		if err != nil {
			return nil, err
		}

		agentID := strings.TrimSpace(valueString(req.Args, "agent_id"))
		statusFilter := strings.TrimSpace(strings.ToLower(valueString(req.Args, "status")))

		filtered := filterRuns(runs, agentID, statusFilter)

		limit := getIntArg(req.Args, "limit", defaultMetricsRunLimit)
		if limit <= 0 {
			limit = defaultMetricsRunLimit
		}
		if limit > maxMetricsRunLimit {
			limit = maxMetricsRunLimit
		}
		offset := getIntArg(req.Args, "offset", 0)

		window := paginateRuns(filtered, limit, offset)

		runStatusCounts, toolCallsTotal, tools := calculateToolStats(window)

		return map[string]any{
			"generated_at": time.Now().UTC().Format(time.RFC3339),
			"filter": map[string]any{
				"agent_id": agentID,
				"status":   statusFilter,
				"limit":    limit,
				"offset":   offset,
			},
			"runs": map[string]any{
				"total":         len(filtered),
				"window_count":  len(window),
				"status_counts": runStatusCounts,
			},
			"tool_calls_total": toolCallsTotal,
			"tools":            tools,
		}, nil
	}
}

func filterRuns(runs []httpchannel.Run, agentID, statusFilter string) []httpchannel.Run {
	filtered := make([]httpchannel.Run, 0, len(runs))
	for _, run := range runs {
		if agentID != "" && run.AgentID != agentID {
			continue
		}
		if statusFilter != "" && strings.ToLower(strings.TrimSpace(run.Status)) != statusFilter {
			continue
		}
		filtered = append(filtered, run)
	}
	return filtered
}

func paginateRuns(runs []httpchannel.Run, limit, offset int) []httpchannel.Run {
	if offset < 0 {
		offset = 0
	}
	if offset > len(runs) {
		offset = len(runs)
	}
	end := offset + limit
	if end > len(runs) {
		end = len(runs)
	}
	return runs[offset:end]
}

func calculateToolStats(window []httpchannel.Run) (map[string]int, int, []map[string]any) {
	runStatusCounts := map[string]int{}
	toolStats := map[string]map[string]any{}
	toolCallsTotal := 0
	for _, run := range window {
		status := strings.ToLower(strings.TrimSpace(run.Status))
		if status == "" {
			status = "unknown"
		}
		runStatusCounts[status]++
		toolCallsTotal += run.ToolCalls

		rawItems, ok := run.Trace["tool_execution_results"].([]any)
		if !ok {
			continue
		}
		for _, item := range rawItems {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			tool := strings.TrimSpace(fmt.Sprintf("%v", entry["tool"]))
			if tool == "" {
				continue
			}
			stats, ok := toolStats[tool]
			if !ok {
				stats = map[string]any{"tool": tool, "calls": 0, "errors": 0, "total_duration_ms": int64(0), "max_duration_ms": int64(0)}
				toolStats[tool] = stats
			}
			stats["calls"] = stats["calls"].(int) + 1
			errText := strings.TrimSpace(fmt.Sprintf("%v", entry["error"]))
			callbackErr := strings.TrimSpace(fmt.Sprintf("%v", entry["callback_error"]))
			if errText != "" && errText != "<nil>" || callbackErr != "" && callbackErr != "<nil>" {
				stats["errors"] = stats["errors"].(int) + 1
			}
			dur := int64(getIntArg(entry, "duration_ms", 0))
			if dur < 0 {
				dur = 0
			}
			totalDur := stats["total_duration_ms"].(int64) + dur
			stats["total_duration_ms"] = totalDur
			if dur > stats["max_duration_ms"].(int64) {
				stats["max_duration_ms"] = dur
			}
		}
	}

	tools := make([]map[string]any, 0, len(toolStats))
	for _, stats := range toolStats {
		calls := stats["calls"].(int)
		totalDur := stats["total_duration_ms"].(int64)
		avg := int64(0)
		if calls > 0 {
			avg = totalDur / int64(calls)
		}
		tools = append(tools, map[string]any{
			"tool":              stats["tool"],
			"calls":             calls,
			"errors":            stats["errors"],
			"total_duration_ms": totalDur,
			"avg_duration_ms":   avg,
			"max_duration_ms":   stats["max_duration_ms"],
		})
	}
	sort.Slice(tools, func(i, j int) bool {
		left, _ := tools[i]["tool"].(string)
		right, _ := tools[j]["tool"].(string)
		return left < right
	})
	return runStatusCounts, toolCallsTotal, tools
}
