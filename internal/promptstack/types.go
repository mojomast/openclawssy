package promptstack

import (
	"errors"
	"strings"
	"time"
)

const (
	LayerGlobalOperatorPolicy = "global_operator_policy"
	LayerAgentIdentity        = "agent_identity"
	LayerToolSafetyRules      = "tool_safety_rules"
	LayerDelegationPolicy     = "delegation_policy"
	LayerSessionOverlay       = "session_overlay"
)

var (
	ErrInvalidLayerID = errors.New("promptstack: invalid layer id")

	layerOrder = []string{
		LayerGlobalOperatorPolicy,
		LayerAgentIdentity,
		LayerToolSafetyRules,
		LayerDelegationPolicy,
		LayerSessionOverlay,
	}
)

type PromptLayer struct {
	LayerID   string    `json:"layer_id"`
	Content   string    `json:"content"`
	UpdatedAt time.Time `json:"updated_at"`
	Version   int       `json:"version"`
}

type PromptStack struct {
	GlobalOperatorPolicy PromptLayer `json:"global_operator_policy"`
	AgentIdentity        PromptLayer `json:"agent_identity"`
	ToolSafetyRules      PromptLayer `json:"tool_safety_rules"`
	DelegationPolicy     PromptLayer `json:"delegation_policy"`
	SessionOverlay       PromptLayer `json:"session_overlay"`
}

func NewPromptStack() PromptStack {
	return PromptStack{
		GlobalOperatorPolicy: PromptLayer{LayerID: LayerGlobalOperatorPolicy},
		AgentIdentity:        PromptLayer{LayerID: LayerAgentIdentity},
		ToolSafetyRules:      PromptLayer{LayerID: LayerToolSafetyRules},
		DelegationPolicy:     PromptLayer{LayerID: LayerDelegationPolicy},
		SessionOverlay:       PromptLayer{LayerID: LayerSessionOverlay},
	}
}

func LayerIDs() []string {
	out := make([]string, len(layerOrder))
	copy(out, layerOrder)
	return out
}

func (s PromptStack) LayersInOrder() []PromptLayer {
	return []PromptLayer{
		s.GlobalOperatorPolicy,
		s.AgentIdentity,
		s.ToolSafetyRules,
		s.DelegationPolicy,
		s.SessionOverlay,
	}
}

func (s PromptStack) Layer(layerID string) (PromptLayer, error) {
	layerID = strings.TrimSpace(layerID)
	switch layerID {
	case LayerGlobalOperatorPolicy:
		return s.GlobalOperatorPolicy, nil
	case LayerAgentIdentity:
		return s.AgentIdentity, nil
	case LayerToolSafetyRules:
		return s.ToolSafetyRules, nil
	case LayerDelegationPolicy:
		return s.DelegationPolicy, nil
	case LayerSessionOverlay:
		return s.SessionOverlay, nil
	default:
		return PromptLayer{}, ErrInvalidLayerID
	}
}

func (s *PromptStack) SetLayer(layer PromptLayer) error {
	layerID := strings.TrimSpace(layer.LayerID)
	if !isValidLayerID(layerID) {
		return ErrInvalidLayerID
	}
	layer.LayerID = layerID

	switch layerID {
	case LayerGlobalOperatorPolicy:
		s.GlobalOperatorPolicy = layer
	case LayerAgentIdentity:
		s.AgentIdentity = layer
	case LayerToolSafetyRules:
		s.ToolSafetyRules = layer
	case LayerDelegationPolicy:
		s.DelegationPolicy = layer
	case LayerSessionOverlay:
		s.SessionOverlay = layer
	default:
		return ErrInvalidLayerID
	}

	return nil
}

func isValidLayerID(layerID string) bool {
	switch strings.TrimSpace(layerID) {
	case LayerGlobalOperatorPolicy, LayerAgentIdentity, LayerToolSafetyRules, LayerDelegationPolicy, LayerSessionOverlay:
		return true
	default:
		return false
	}
}
