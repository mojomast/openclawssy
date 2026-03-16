package instances

import (
	"errors"
	"path/filepath"
	"strings"
)

const (
	instanceManifestFileName = "manifest.json"
	agentManifestFileName    = "agent.json"
	instanceRolesFileName    = "roles.json"
	instanceSkillsFileName   = "skills.json"
	instanceChannelsFileName = "channels.json"
	agentDocsDirName         = "docs"
	agentPromptstackDirName  = "promptstack"
	identityFileName         = "identity.json"
)

var (
	ErrInvalidInstanceID = errors.New("instances: invalid instance id")
	ErrInvalidAgentID    = errors.New("instances: invalid agent id")
)

func ControlPlaneDir(rootDir string) string {
	return filepath.Join(strings.TrimSpace(rootDir), ".openclawssy")
}

func LegacyAgentsDir(rootDir string) string {
	return filepath.Join(ControlPlaneDir(rootDir), "agents")
}

func LegacyAgentDir(rootDir, agentID string) (string, error) {
	normalized, err := ValidateAgentID(agentID)
	if err != nil {
		return "", err
	}
	return filepath.Join(LegacyAgentsDir(rootDir), normalized), nil
}

func InstancesDir(rootDir string) string {
	return filepath.Join(ControlPlaneDir(rootDir), "instances")
}

func InstanceDir(rootDir, instanceID string) (string, error) {
	normalized, err := ValidateInstanceID(instanceID)
	if err != nil {
		return "", err
	}
	return filepath.Join(InstancesDir(rootDir), normalized), nil
}

func InstanceManifestPath(rootDir, instanceID string) (string, error) {
	instanceDir, err := InstanceDir(rootDir, instanceID)
	if err != nil {
		return "", err
	}
	return filepath.Join(instanceDir, instanceManifestFileName), nil
}

func AgentsDir(rootDir, instanceID string) (string, error) {
	instanceDir, err := InstanceDir(rootDir, instanceID)
	if err != nil {
		return "", err
	}
	return filepath.Join(instanceDir, "agents"), nil
}

func AgentDir(rootDir, instanceID, agentID string) (string, error) {
	agentsDir, err := AgentsDir(rootDir, instanceID)
	if err != nil {
		return "", err
	}
	normalized, err := ValidateAgentID(agentID)
	if err != nil {
		return "", err
	}
	return filepath.Join(agentsDir, normalized), nil
}

func AgentManifestPath(rootDir, instanceID, agentID string) (string, error) {
	agentDir, err := AgentDir(rootDir, instanceID, agentID)
	if err != nil {
		return "", err
	}
	return filepath.Join(agentDir, agentManifestFileName), nil
}

func AgentDocsDir(rootDir, instanceID, agentID string) (string, error) {
	agentDir, err := AgentDir(rootDir, instanceID, agentID)
	if err != nil {
		return "", err
	}
	return filepath.Join(agentDir, agentDocsDirName), nil
}

func AgentPromptStackDir(rootDir, instanceID, agentID string) (string, error) {
	agentDir, err := AgentDir(rootDir, instanceID, agentID)
	if err != nil {
		return "", err
	}
	return filepath.Join(agentDir, agentPromptstackDirName), nil
}

func InstanceRolesPath(rootDir, instanceID string) (string, error) {
	instanceDir, err := InstanceDir(rootDir, instanceID)
	if err != nil {
		return "", err
	}
	return filepath.Join(instanceDir, instanceRolesFileName), nil
}

func InstanceSkillsPath(rootDir, instanceID string) (string, error) {
	instanceDir, err := InstanceDir(rootDir, instanceID)
	if err != nil {
		return "", err
	}
	return filepath.Join(instanceDir, instanceSkillsFileName), nil
}

func InstanceChannelsPath(rootDir, instanceID string) (string, error) {
	instanceDir, err := InstanceDir(rootDir, instanceID)
	if err != nil {
		return "", err
	}
	return filepath.Join(instanceDir, instanceChannelsFileName), nil
}

func AgentDocPath(rootDir, instanceID, agentID, name string) (string, error) {
	docsDir, err := AgentDocsDir(rootDir, instanceID, agentID)
	if err != nil {
		return "", err
	}
	canonical, ok := NormalizeAgentDocName(name)
	if !ok {
		return "", errors.New("instances: unsupported agent document name")
	}
	return filepath.Join(docsDir, canonical), nil
}

func AgentIdentityPath(rootDir, instanceID, agentID string) (string, error) {
	agentDir, err := AgentDir(rootDir, instanceID, agentID)
	if err != nil {
		return "", err
	}
	return filepath.Join(agentDir, identityFileName), nil
}

func ValidateInstanceID(raw string) (string, error) {
	return validateStorageID(raw, ErrInvalidInstanceID)
}

func ValidateAgentID(raw string) (string, error) {
	return validateStorageID(raw, ErrInvalidAgentID)
}

func validateStorageID(raw string, errInvalid error) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errInvalid
	}
	if strings.Contains(trimmed, "..") || strings.ContainsRune(trimmed, '/') || strings.ContainsRune(trimmed, '\\') {
		return "", errInvalid
	}
	return trimmed, nil
}
