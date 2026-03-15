package agent

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"openclawssy/internal/roles"
)

const trustedDelegationConfidenceThreshold = 0.7

type DecompositionTaskNode struct {
	TaskID             string   `json:"task_id"`
	Description        string   `json:"description"`
	AssignedRole       string   `json:"assigned_role"`
	Confidence         float64  `json:"confidence"`
	ExpectedArtifacts  []string `json:"expected_artifacts,omitempty"`
	CompletionCriteria []string `json:"completion_criteria,omitempty"`
	DependsOn          []string `json:"depends_on,omitempty"`
	Rationale          string   `json:"rationale,omitempty"`
}

type PlanDependencyEdge struct {
	FromTaskID string `json:"from_task_id"`
	ToTaskID   string `json:"to_task_id"`
}

type DecompositionPlan struct {
	DelegationMode  string                  `json:"delegation_mode,omitempty"`
	TriggerReason   string                  `json:"trigger_reason,omitempty"`
	Tasks           []DecompositionTaskNode `json:"tasks"`
	DependencyDAG   []PlanDependencyEdge    `json:"dependency_dag,omitempty"`
	MinConfidence   float64                 `json:"min_confidence"`
	AvgConfidence   float64                 `json:"avg_confidence"`
	AllRolesBuiltIn bool                    `json:"all_roles_built_in"`
	GeneratedAt     time.Time               `json:"generated_at"`
}

type DelegationEvent struct {
	Timestamp      time.Time `json:"timestamp"`
	TaskID         string    `json:"task_id,omitempty"`
	TriggerReason  string    `json:"trigger_reason"`
	SelectedRole   string    `json:"selected_role"`
	Confidence     float64   `json:"confidence"`
	TaskAssignment string    `json:"task_assignment"`
	Rationale      string    `json:"rationale"`
	Outcome        string    `json:"outcome,omitempty"`
}

var builtInRoleNames = func() map[string]struct{} {
	builtIns := roles.BuiltInTemplates()
	set := make(map[string]struct{}, len(builtIns))
	for _, role := range builtIns {
		name := strings.TrimSpace(role.Name)
		if name == "" {
			continue
		}
		set[name] = struct{}{}
	}
	return set
}()

func GenerateDecompositionPlan(triggerReason string, mode DelegationMode, tasks []DecomposedTask, router *roles.Router) (DecompositionPlan, []DecomposedTask, error) {
	if len(tasks) == 0 {
		return DecompositionPlan{}, nil, fmt.Errorf("delegation planner: no tasks to plan")
	}
	if router == nil {
		router = roles.NewRouter(nil)
	}

	plannedTasks := make([]DecomposedTask, 0, len(tasks))
	allBuiltIn := true
	minConfidence := 1.0
	totalConfidence := 0.0

	for idx, task := range tasks {
		if strings.TrimSpace(task.TaskID) == "" {
			task.TaskID = fmt.Sprintf("task-%d", idx+1)
		}
		if strings.TrimSpace(task.Message) == "" {
			task.Message = fmt.Sprintf("Complete delegated task %d", idx+1)
		}

		routed := applyRoleRouting(task, router)
		confidence := routed.RoutingConfidence
		if confidence <= 0 {
			confidence = 0.2
			routed.RoutingConfidence = confidence
		}
		totalConfidence += confidence
		if confidence < minConfidence {
			minConfidence = confidence
		}
		if _, ok := builtInRoleNames[strings.TrimSpace(routed.AssignedRole)]; !ok {
			allBuiltIn = false
		}

		plannedTasks = append(plannedTasks, routed)
	}

	ordered, err := topologicalSortTasks(plannedTasks)
	if err != nil {
		return DecompositionPlan{}, nil, err
	}

	edges := make([]PlanDependencyEdge, 0, len(ordered))
	nodes := make([]DecompositionTaskNode, 0, len(ordered))
	for _, task := range ordered {
		nodes = append(nodes, DecompositionTaskNode{
			TaskID:             task.TaskID,
			Description:        strings.TrimSpace(task.Message),
			AssignedRole:       strings.TrimSpace(task.AssignedRole),
			Confidence:         task.RoutingConfidence,
			ExpectedArtifacts:  append([]string(nil), task.Produces...),
			CompletionCriteria: append([]string(nil), task.AcceptanceCrit...),
			DependsOn:          append([]string(nil), task.DependsOn...),
			Rationale:          strings.TrimSpace(task.RoutingRationale),
		})
		for _, dep := range task.DependsOn {
			edges = append(edges, PlanDependencyEdge{FromTaskID: dep, ToTaskID: task.TaskID})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].FromTaskID == edges[j].FromTaskID {
			return edges[i].ToTaskID < edges[j].ToTaskID
		}
		return edges[i].FromTaskID < edges[j].FromTaskID
	})

	avgConfidence := totalConfidence / float64(len(ordered))
	plan := DecompositionPlan{
		DelegationMode:  strings.TrimSpace(string(mode)),
		TriggerReason:   strings.TrimSpace(triggerReason),
		Tasks:           nodes,
		DependencyDAG:   edges,
		MinConfidence:   minConfidence,
		AvgConfidence:   avgConfidence,
		AllRolesBuiltIn: allBuiltIn,
		GeneratedAt:     time.Now().UTC(),
	}

	return plan, ordered, nil
}

func isPlannerDelegationMode(mode DelegationMode) bool {
	switch mode {
	case DelegationModeSuggestOnly, DelegationModeApprovePlan, DelegationModeAutoTrusted, DelegationModeFullAuto:
		return true
	default:
		return false
	}
}

func normalizeDelegationMode(raw string) DelegationMode {
	mode := DelegationMode(strings.ToLower(strings.TrimSpace(raw)))
	switch mode {
	case DelegationModePromptOnly, DelegationModeToolGated, DelegationModeAutoExecute,
		DelegationModeSuggestOnly, DelegationModeApprovePlan, DelegationModeAutoTrusted, DelegationModeFullAuto:
		return mode
	default:
		return ""
	}
}

func shouldAutoExecutePlan(mode DelegationMode, plan DecompositionPlan, approved bool) (bool, string) {
	switch mode {
	case DelegationModeFullAuto:
		return true, "full_autonomous mode always executes delegation plan"
	case DelegationModeAutoTrusted:
		if plan.AllRolesBuiltIn && plan.MinConfidence > trustedDelegationConfidenceThreshold {
			return true, "auto_trusted mode approved: all roles built-in and confidence > 0.7"
		}
		return false, "auto_trusted mode withheld execution because roles/confidence did not satisfy trust threshold"
	case DelegationModeApprovePlan:
		if approved {
			return true, "approve_plan mode received explicit approval"
		}
		return false, "approve_plan mode requires operator approval"
	case DelegationModeSuggestOnly:
		return false, "suggest_only mode returns plan without execution"
	default:
		return false, "delegation mode does not support planner-led auto execution"
	}
}

func formatDecompositionPlanForOperator(plan DecompositionPlan, summary string) string {
	var b strings.Builder
	b.WriteString("# Delegation Plan\n\n")
	b.WriteString("- mode: ")
	b.WriteString(strings.TrimSpace(plan.DelegationMode))
	b.WriteString("\n")
	b.WriteString("- trigger_reason: ")
	b.WriteString(strings.TrimSpace(plan.TriggerReason))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("- min_confidence: %.2f\n", plan.MinConfidence))
	b.WriteString(fmt.Sprintf("- avg_confidence: %.2f\n", plan.AvgConfidence))
	b.WriteString(fmt.Sprintf("- all_roles_built_in: %t\n", plan.AllRolesBuiltIn))
	if trimmed := strings.TrimSpace(summary); trimmed != "" {
		b.WriteString("- decision: ")
		b.WriteString(trimmed)
		b.WriteString("\n")
	}
	b.WriteString("\n## Tasks\n")
	for _, task := range plan.Tasks {
		b.WriteString("\n- ")
		b.WriteString(task.TaskID)
		b.WriteString(" [")
		b.WriteString(task.AssignedRole)
		b.WriteString("]")
		b.WriteString(fmt.Sprintf(" confidence=%.2f", task.Confidence))
		if strings.TrimSpace(task.Description) != "" {
			b.WriteString("\n  - description: ")
			b.WriteString(task.Description)
		}
		if len(task.DependsOn) > 0 {
			b.WriteString("\n  - depends_on: ")
			b.WriteString(strings.Join(task.DependsOn, ", "))
		}
		if len(task.CompletionCriteria) > 0 {
			b.WriteString("\n  - completion_criteria: ")
			b.WriteString(strings.Join(task.CompletionCriteria, "; "))
		}
		if len(task.ExpectedArtifacts) > 0 {
			b.WriteString("\n  - expected_artifacts: ")
			b.WriteString(strings.Join(task.ExpectedArtifacts, ", "))
		}
		if strings.TrimSpace(task.Rationale) != "" {
			b.WriteString("\n  - rationale: ")
			b.WriteString(task.Rationale)
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}
