package tools

import (
	"context"
	"errors"
	"sort"
	"strings"

	"openclawssy/internal/policy"
)

const (
	defaultPolicyListLimit = 50
	maxPolicyListLimit     = 200
)

func registerPolicyTools(reg *Registry, policyPath string, defaultGrants []string) error {
	if err := reg.Register(ToolSpec{
		Name:        "policy.list",
		Description: "List policy capabilities by agent",
		ArgTypes: map[string]ArgType{
			"agent_id": ArgTypeString,
			"limit":    ArgTypeNumber,
			"offset":   ArgTypeNumber,
		},
	}, policyList(policyPath, defaultGrants)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "policy.grant",
		Description: "Grant a capability to an agent",
		Required:    []string{"agent_id", "capability"},
		ArgTypes: map[string]ArgType{
			"agent_id":   ArgTypeString,
			"capability": ArgTypeString,
			"tool":       ArgTypeString,
		},
	}, policyGrant(policyPath, defaultGrants)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "policy.revoke",
		Description: "Revoke a capability from an agent",
		Required:    []string{"agent_id", "capability"},
		ArgTypes: map[string]ArgType{
			"agent_id":   ArgTypeString,
			"capability": ArgTypeString,
			"tool":       ArgTypeString,
		},
	}, policyRevoke(policyPath, defaultGrants)); err != nil {
		return err
	}
	return nil
}

func policyList(configuredPath string, defaultGrants []string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		if err := requirePolicyAdmin(req); err != nil {
			return nil, err
		}
		path, err := resolveOpenClawssyPath(req.Workspace, configuredPath, "policy", "policy", "capabilities.json")
		if err != nil {
			return nil, err
		}
		grants, err := policy.LoadGrants(path)
		if err != nil {
			return nil, err
		}

		agentID := strings.TrimSpace(valueString(req.Args, "agent_id"))
		if agentID != "" {
			agentID, err = validatedAgentID(agentID)
			if err != nil {
				return nil, err
			}
			caps, source := effectivePolicyCapabilities(agentID, defaultGrants, grants)
			return map[string]any{
				"agent_id":     agentID,
				"capabilities": caps,
				"source":       source,
			}, nil
		}

		idsSet := map[string]struct{}{"default": {}}
		for id := range grants {
			idsSet[id] = struct{}{}
		}
		ids := make([]string, 0, len(idsSet))
		for id := range idsSet {
			ids = append(ids, id)
		}
		sort.Strings(ids)

		limit := getIntArg(req.Args, "limit", defaultPolicyListLimit)
		if limit <= 0 {
			limit = defaultPolicyListLimit
		}
		if limit > maxPolicyListLimit {
			limit = maxPolicyListLimit
		}
		offset := getIntArg(req.Args, "offset", 0)
		if offset < 0 {
			offset = 0
		}
		if offset > len(ids) {
			offset = len(ids)
		}
		end := offset + limit
		if end > len(ids) {
			end = len(ids)
		}

		items := make([]map[string]any, 0, end-offset)
		for _, id := range ids[offset:end] {
			caps, source := effectivePolicyCapabilities(id, defaultGrants, grants)
			items = append(items, map[string]any{
				"agent_id":     id,
				"capabilities": caps,
				"source":       source,
			})
		}

		return map[string]any{
			"total":  len(ids),
			"count":  len(items),
			"limit":  limit,
			"offset": offset,
			"items":  items,
		}, nil
	}
}

func policyGrant(configuredPath string, defaultGrants []string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		if err := requirePolicyAdmin(req); err != nil {
			return nil, err
		}
		targetAgent, capability, err := parsePolicyMutationArgs(req.Args)
		if err != nil {
			return nil, err
		}
		path, err := resolveOpenClawssyPath(req.Workspace, configuredPath, "policy", "policy", "capabilities.json")
		if err != nil {
			return nil, err
		}
		grants, err := policy.LoadGrants(path)
		if err != nil {
			return nil, err
		}

		current, _ := effectivePolicyCapabilities(targetAgent, defaultGrants, grants)
		beforeLen := len(current)
		updated := policy.NormalizeCapabilities(append(current, capability))
		grants[targetAgent] = updated
		if err := policy.SaveGrants(path, grants); err != nil {
			return nil, err
		}
		if enforcer, ok := req.Policy.(*policy.Enforcer); ok {
			enforcer.SetCapabilities(targetAgent, updated)
		}

		return map[string]any{
			"agent_id":       targetAgent,
			"capability":     capability,
			"granted":        true,
			"changed":        len(updated) > beforeLen,
			"capabilities":   updated,
			"capability_cnt": len(updated),
		}, nil
	}
}

func policyRevoke(configuredPath string, defaultGrants []string) Handler {
	return func(_ context.Context, req Request) (map[string]any, error) {
		if err := requirePolicyAdmin(req); err != nil {
			return nil, err
		}
		targetAgent, capability, err := parsePolicyMutationArgs(req.Args)
		if err != nil {
			return nil, err
		}
		path, err := resolveOpenClawssyPath(req.Workspace, configuredPath, "policy", "policy", "capabilities.json")
		if err != nil {
			return nil, err
		}
		grants, err := policy.LoadGrants(path)
		if err != nil {
			return nil, err
		}

		current, _ := effectivePolicyCapabilities(targetAgent, defaultGrants, grants)
		filtered := make([]string, 0, len(current))
		removed := false
		for _, existing := range current {
			if existing == capability {
				removed = true
				continue
			}
			filtered = append(filtered, existing)
		}
		updated := policy.NormalizeCapabilities(filtered)
		grants[targetAgent] = updated
		if err := policy.SaveGrants(path, grants); err != nil {
			return nil, err
		}
		if enforcer, ok := req.Policy.(*policy.Enforcer); ok {
			enforcer.SetCapabilities(targetAgent, updated)
		}

		return map[string]any{
			"agent_id":       targetAgent,
			"capability":     capability,
			"revoked":        true,
			"changed":        removed,
			"capabilities":   updated,
			"capability_cnt": len(updated),
		}, nil
	}
}

func requirePolicyAdmin(req Request) error {
	if req.Policy == nil {
		return &ToolError{Code: ErrCodePolicyDenied, Tool: req.Tool, Message: "policy enforcer is required"}
	}
	if err := req.Policy.CheckTool(req.AgentID, "policy.admin"); err != nil {
		return &ToolError{Code: ErrCodePolicyDenied, Tool: req.Tool, Message: err.Error(), Cause: err}
	}
	return nil
}

func parsePolicyMutationArgs(args map[string]any) (string, string, error) {
	agentID, err := validatedAgentID(valueString(args, "agent_id"))
	if err != nil {
		return "", "", err
	}
	capability := strings.TrimSpace(valueString(args, "capability"))
	if capability == "" {
		capability = strings.TrimSpace(valueString(args, "tool"))
	}
	capability = policy.CanonicalCapability(capability)
	if capability == "" {
		return "", "", errors.New("capability is required")
	}
	return agentID, capability, nil
}

func effectivePolicyCapabilities(agentID string, defaultGrants []string, persisted map[string][]string) ([]string, string) {
	if custom, ok := persisted[agentID]; ok {
		return policy.NormalizeCapabilities(custom), "persisted"
	}
	base := policy.NormalizeCapabilities(defaultGrants)
	if agentID == "default" {
		base = policy.NormalizeCapabilities(append(base, "policy.admin"))
	}
	return base, "default"
}
