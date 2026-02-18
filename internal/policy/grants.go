package policy

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"

	"openclawssy/internal/fsutil"
)

type grantsFile struct {
	Agents map[string][]string `json:"agents"`
}

func LoadGrants(path string) (map[string][]string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return map[string][]string{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string][]string{}, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return map[string][]string{}, nil
	}
	var doc grantsFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(doc.Agents))
	for agentID, capabilities := range doc.Agents {
		agentID = strings.TrimSpace(agentID)
		if agentID == "" {
			continue
		}
		out[agentID] = NormalizeCapabilities(capabilities)
	}
	return out, nil
}

func SaveGrants(path string, grants map[string][]string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	clean := make(map[string][]string, len(grants))
	for agentID, capabilities := range grants {
		agentID = strings.TrimSpace(agentID)
		if agentID == "" {
			continue
		}
		clean[agentID] = NormalizeCapabilities(capabilities)
	}

	doc := grantsFile{Agents: clean}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	return fsutil.WriteFileAtomic(path, raw, 0o600)
}

func NormalizeCapabilities(capabilities []string) []string {
	set := map[string]struct{}{}
	for _, capability := range capabilities {
		canonical := CanonicalCapability(capability)
		if canonical == "" {
			continue
		}
		set[canonical] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for capability := range set {
		out = append(out, capability)
	}
	sort.Strings(out)
	return out
}
