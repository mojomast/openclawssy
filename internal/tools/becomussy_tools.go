package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"openclawssy/internal/config"
)

const (
	becomussyMaxResponseBytes = 2 * 1024 * 1024 // 2MB
)

func registerBecomussyTools(reg *Registry, configPath string) error {
	if err := reg.Register(ToolSpec{
		Name:        "becomussy.resume",
		Description: "Load continuity resume bundle from becomussy (threads, commitments, memories, identity changes)",
		ArgTypes: map[string]ArgType{
			"query":        ArgTypeString,
			"token_budget": ArgTypeNumber,
		},
	}, becomussyResume(configPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "becomussy.memory.create",
		Description: "Store a new memory item in becomussy",
		Required:    []string{"memory_type"},
		ArgTypes: map[string]ArgType{
			"memory_type":      ArgTypeString,
			"summary":          ArgTypeString,
			"statement":        ArgTypeString,
			"importance_score": ArgTypeNumber,
			"confidence_level": ArgTypeString,
			"metadata":         ArgTypeObject,
			"source_kind":      ArgTypeString,
			"source_ref":       ArgTypeString,
		},
	}, becomussyMemoryCreate(configPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "becomussy.memory.search",
		Description: "Search memory items in becomussy",
		ArgTypes: map[string]ArgType{
			"q":           ArgTypeString,
			"memory_type": ArgTypeString,
			"date_from":   ArgTypeString,
			"date_to":     ArgTypeString,
			"confidence":  ArgTypeString,
			"limit":       ArgTypeNumber,
			"offset":      ArgTypeNumber,
		},
	}, becomussyMemorySearch(configPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "becomussy.memory.get",
		Description: "Get a specific memory item by ID from becomussy",
		Required:    []string{"id"},
		ArgTypes: map[string]ArgType{
			"id": ArgTypeString,
		},
	}, becomussySimpleGet(configPath, "/api/v1/memory/%s", "id")); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "becomussy.memory.reinforce",
		Description: "Reinforce a memory item (bump salience) in becomussy",
		Required:    []string{"id", "reason"},
		ArgTypes: map[string]ArgType{
			"id":         ArgTypeString,
			"reason":     ArgTypeString,
			"source_ref": ArgTypeString,
		},
	}, becomussyMemoryReinforce(configPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "becomussy.journal.create",
		Description: "Write a journal/reflection entry in becomussy",
		Required:    []string{"entry_type", "title", "body_md"},
		ArgTypes: map[string]ArgType{
			"entry_type":             ArgTypeString,
			"title":                  ArgTypeString,
			"body_md":                ArgTypeString,
			"confidence_level":       ArgTypeString,
			"tags":                   ArgTypeArray,
			"linked_memory_ids":      ArgTypeArray,
			"linked_project_ids":     ArgTypeArray,
			"linked_identity_themes": ArgTypeArray,
		},
	}, becomussyJournalCreate(configPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "becomussy.journal.search",
		Description: "Search journal entries in becomussy",
		ArgTypes: map[string]ArgType{
			"keyword":           ArgTypeString,
			"entry_type":        ArgTypeString,
			"date_from":         ArgTypeString,
			"date_to":           ArgTypeString,
			"linked_project_id": ArgTypeString,
			"linked_theme":      ArgTypeString,
			"limit":             ArgTypeNumber,
			"offset":            ArgTypeNumber,
		},
	}, becomussyJournalSearch(configPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "becomussy.threads.list",
		Description: "List active threads in becomussy",
		ArgTypes: map[string]ArgType{
			"status":      ArgTypeString,
			"thread_type": ArgTypeString,
			"limit":       ArgTypeNumber,
			"offset":      ArgTypeNumber,
		},
	}, becomussyThreadsList(configPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "becomussy.threads.create",
		Description: "Create a new thread in becomussy",
		Required:    []string{"title"},
		ArgTypes: map[string]ArgType{
			"title":       ArgTypeString,
			"description": ArgTypeString,
			"thread_type": ArgTypeString,
			"urgency":     ArgTypeNumber,
			"importance":  ArgTypeNumber,
			"next_action": ArgTypeString,
			"blocker":     ArgTypeString,
		},
	}, becomussyThreadsCreate(configPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "becomussy.projects.list",
		Description: "List projects in becomussy",
		ArgTypes: map[string]ArgType{
			"status": ArgTypeString,
			"limit":  ArgTypeNumber,
			"offset": ArgTypeNumber,
		},
	}, becomussyProjectsList(configPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "becomussy.projects.create",
		Description: "Create a new project in becomussy",
		Required:    []string{"name"},
		ArgTypes: map[string]ArgType{
			"name":          ArgTypeString,
			"purpose":       ArgTypeString,
			"origin":        ArgTypeString,
			"current_phase": ArgTypeString,
			"linked_themes": ArgTypeArray,
			"linked_people": ArgTypeArray,
			"status":        ArgTypeString,
		},
	}, becomussyProjectsCreate(configPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "becomussy.selfmodel.current",
		Description: "Get the current self-model version from becomussy",
	}, becomussySimpleGetNoID(configPath, "/api/v1/self-model/current")); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "becomussy.selfmodel.history",
		Description: "Get self-model version history from becomussy",
	}, becomussySimpleGetNoID(configPath, "/api/v1/self-model/history")); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "becomussy.selfmodel.propose",
		Description: "Create a revision proposal for the self-model in becomussy",
		Required:    []string{"revision_type", "target_entity_type", "summary"},
		ArgTypes: map[string]ArgType{
			"revision_type":      ArgTypeString,
			"target_entity_type": ArgTypeString,
			"target_entity_id":   ArgTypeString,
			"summary":            ArgTypeString,
			"rationale":          ArgTypeString,
			"evidence_links":     ArgTypeArray,
			"proposed_diff":      ArgTypeObject,
		},
	}, becomussySelfModelPropose(configPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "becomussy.commitments.list",
		Description: "List commitments in becomussy",
		ArgTypes: map[string]ArgType{
			"project_id": ArgTypeString,
			"status":     ArgTypeString,
			"overdue":    ArgTypeBool,
			"limit":      ArgTypeNumber,
			"offset":     ArgTypeNumber,
		},
	}, becomussyCommitmentsList(configPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "becomussy.commitments.create",
		Description: "Create a new commitment in becomussy",
		Required:    []string{"commitment_text"},
		ArgTypes: map[string]ArgType{
			"project_id":      ArgTypeString,
			"commitment_text": ArgTypeString,
			"made_to":         ArgTypeString,
			"due_date":        ArgTypeString,
			"risk_if_missed":  ArgTypeString,
		},
	}, becomussyCommitmentsCreate(configPath)); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "becomussy.approvals.pending",
		Description: "List pending approval items in becomussy governance",
	}, becomussySimpleGetNoID(configPath, "/api/v1/approvals/pending")); err != nil {
		return err
	}
	if err := reg.Register(ToolSpec{
		Name:        "becomussy.audit.list",
		Description: "List audit events from becomussy",
		ArgTypes: map[string]ArgType{
			"entity_type": ArgTypeString,
			"event_type":  ArgTypeString,
			"actor":       ArgTypeString,
			"limit":       ArgTypeNumber,
			"offset":      ArgTypeNumber,
		},
	}, becomussyAuditList(configPath)); err != nil {
		return err
	}
	return nil
}

// ----- helper: load becomussy config and build HTTP client -----

type becomussyClient struct {
	baseURL string
	userID  string
	role    string
	headers map[string]string
	client  *http.Client
}

func newBecomussyClient(req Request, configPath string) (*becomussyClient, error) {
	cfgPath, err := resolveOpenClawssyPath(req.Workspace, configPath, "config", "config.json")
	if err != nil {
		return nil, err
	}
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		return nil, err
	}
	if !cfg.Becomussy.Enabled {
		return nil, errors.New("becomussy integration is disabled (set becomussy.enabled=true in config)")
	}
	timeout := time.Duration(cfg.Becomussy.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &becomussyClient{
		baseURL: strings.TrimRight(cfg.Becomussy.BaseURL, "/"),
		userID:  cfg.Becomussy.UserID,
		role:    cfg.Becomussy.UserRole,
		headers: cfg.Becomussy.Headers,
		client:  &http.Client{Timeout: timeout},
	}, nil
}

func (bc *becomussyClient) doJSON(ctx context.Context, method, path string, body any) (map[string]any, error) {
	urlStr := bc.baseURL + path

	var bodyReader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("becomussy: failed to encode request body: %w", err)
		}
		bodyReader = bytes.NewReader(encoded)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, urlStr, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("becomussy: failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-User-Id", bc.userID)
	httpReq.Header.Set("X-User-Role", bc.role)
	for k, v := range bc.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := bc.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("becomussy: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, int64(becomussyMaxResponseBytes)))
	if err != nil {
		return nil, fmt.Errorf("becomussy: failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("becomussy: %s %s returned %d: %s", method, path, resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		// Response might be an array; wrap it
		var arr []any
		if arrErr := json.Unmarshal(respBody, &arr); arrErr == nil {
			return map[string]any{"items": arr, "status": resp.StatusCode}, nil
		}
		return map[string]any{"raw": string(respBody), "status": resp.StatusCode}, nil
	}
	result["status"] = resp.StatusCode
	return result, nil
}

func (bc *becomussyClient) doGet(ctx context.Context, path string) (map[string]any, error) {
	return bc.doJSON(ctx, http.MethodGet, path, nil)
}

func (bc *becomussyClient) doPost(ctx context.Context, path string, body any) (map[string]any, error) {
	return bc.doJSON(ctx, http.MethodPost, path, body)
}

// ----- helper: build query string from optional args -----

func becomussyQueryString(args map[string]any, keys []string) string {
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		v, ok := args[key]
		if !ok || v == nil {
			continue
		}
		s := strings.TrimSpace(fmt.Sprintf("%v", v))
		if s == "" || s == "<nil>" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", key, s))
	}
	if len(parts) == 0 {
		return ""
	}
	return "?" + strings.Join(parts, "&")
}

// ----- tool handlers -----

func becomussyResume(configPath string) Handler {
	return func(ctx context.Context, req Request) (map[string]any, error) {
		bc, err := newBecomussyClient(req, configPath)
		if err != nil {
			return nil, err
		}
		qs := becomussyQueryString(req.Args, []string{"query", "token_budget"})
		return bc.doGet(ctx, "/api/v1/continuity/resume"+qs)
	}
}

func becomussyMemoryCreate(configPath string) Handler {
	return func(ctx context.Context, req Request) (map[string]any, error) {
		bc, err := newBecomussyClient(req, configPath)
		if err != nil {
			return nil, err
		}
		body := make(map[string]any)
		for _, key := range []string{"memory_type", "summary", "statement", "importance_score", "confidence_level", "metadata", "source_kind", "source_ref"} {
			if v, ok := req.Args[key]; ok && v != nil {
				body[key] = v
			}
		}
		return bc.doPost(ctx, "/api/v1/memory", body)
	}
}

func becomussyMemorySearch(configPath string) Handler {
	return func(ctx context.Context, req Request) (map[string]any, error) {
		bc, err := newBecomussyClient(req, configPath)
		if err != nil {
			return nil, err
		}
		qs := becomussyQueryString(req.Args, []string{"q", "memory_type", "date_from", "date_to", "confidence", "limit", "offset"})
		return bc.doGet(ctx, "/api/v1/memory/search"+qs)
	}
}

func becomussySimpleGet(configPath, pathTemplate, idKey string) Handler {
	return func(ctx context.Context, req Request) (map[string]any, error) {
		bc, err := newBecomussyClient(req, configPath)
		if err != nil {
			return nil, err
		}
		id, err := getString(req.Args, idKey)
		if err != nil {
			return nil, err
		}
		path := fmt.Sprintf(pathTemplate, id)
		return bc.doGet(ctx, path)
	}
}

func becomussySimpleGetNoID(configPath, path string) Handler {
	return func(ctx context.Context, req Request) (map[string]any, error) {
		bc, err := newBecomussyClient(req, configPath)
		if err != nil {
			return nil, err
		}
		return bc.doGet(ctx, path)
	}
}

func becomussyMemoryReinforce(configPath string) Handler {
	return func(ctx context.Context, req Request) (map[string]any, error) {
		bc, err := newBecomussyClient(req, configPath)
		if err != nil {
			return nil, err
		}
		id, err := getString(req.Args, "id")
		if err != nil {
			return nil, err
		}
		body := map[string]any{"reason": req.Args["reason"]}
		if v, ok := req.Args["source_ref"]; ok && v != nil {
			body["source_ref"] = v
		}
		return bc.doPost(ctx, fmt.Sprintf("/api/v1/memory/%s/reinforce", id), body)
	}
}

func becomussyJournalCreate(configPath string) Handler {
	return func(ctx context.Context, req Request) (map[string]any, error) {
		bc, err := newBecomussyClient(req, configPath)
		if err != nil {
			return nil, err
		}
		body := make(map[string]any)
		for _, key := range []string{"entry_type", "title", "body_md", "confidence_level", "tags", "linked_memory_ids", "linked_project_ids", "linked_identity_themes"} {
			if v, ok := req.Args[key]; ok && v != nil {
				body[key] = v
			}
		}
		return bc.doPost(ctx, "/api/v1/journal", body)
	}
}

func becomussyJournalSearch(configPath string) Handler {
	return func(ctx context.Context, req Request) (map[string]any, error) {
		bc, err := newBecomussyClient(req, configPath)
		if err != nil {
			return nil, err
		}
		qs := becomussyQueryString(req.Args, []string{"keyword", "entry_type", "date_from", "date_to", "linked_project_id", "linked_theme", "limit", "offset"})
		return bc.doGet(ctx, "/api/v1/journal/search"+qs)
	}
}

func becomussyThreadsList(configPath string) Handler {
	return func(ctx context.Context, req Request) (map[string]any, error) {
		bc, err := newBecomussyClient(req, configPath)
		if err != nil {
			return nil, err
		}
		qs := becomussyQueryString(req.Args, []string{"status", "thread_type", "limit", "offset"})
		return bc.doGet(ctx, "/api/v1/threads"+qs)
	}
}

func becomussyThreadsCreate(configPath string) Handler {
	return func(ctx context.Context, req Request) (map[string]any, error) {
		bc, err := newBecomussyClient(req, configPath)
		if err != nil {
			return nil, err
		}
		body := make(map[string]any)
		for _, key := range []string{"title", "description", "thread_type", "urgency", "importance", "next_action", "blocker"} {
			if v, ok := req.Args[key]; ok && v != nil {
				body[key] = v
			}
		}
		return bc.doPost(ctx, "/api/v1/threads", body)
	}
}

func becomussyProjectsList(configPath string) Handler {
	return func(ctx context.Context, req Request) (map[string]any, error) {
		bc, err := newBecomussyClient(req, configPath)
		if err != nil {
			return nil, err
		}
		qs := becomussyQueryString(req.Args, []string{"status", "limit", "offset"})
		return bc.doGet(ctx, "/api/v1/projects"+qs)
	}
}

func becomussyProjectsCreate(configPath string) Handler {
	return func(ctx context.Context, req Request) (map[string]any, error) {
		bc, err := newBecomussyClient(req, configPath)
		if err != nil {
			return nil, err
		}
		body := make(map[string]any)
		for _, key := range []string{"name", "purpose", "origin", "current_phase", "linked_themes", "linked_people", "status"} {
			if v, ok := req.Args[key]; ok && v != nil {
				body[key] = v
			}
		}
		return bc.doPost(ctx, "/api/v1/projects", body)
	}
}

func becomussySelfModelPropose(configPath string) Handler {
	return func(ctx context.Context, req Request) (map[string]any, error) {
		bc, err := newBecomussyClient(req, configPath)
		if err != nil {
			return nil, err
		}
		body := make(map[string]any)
		for _, key := range []string{"revision_type", "target_entity_type", "target_entity_id", "summary", "rationale", "evidence_links", "proposed_diff"} {
			if v, ok := req.Args[key]; ok && v != nil {
				body[key] = v
			}
		}
		return bc.doPost(ctx, "/api/v1/self-model/revision-proposal", body)
	}
}

func becomussyCommitmentsList(configPath string) Handler {
	return func(ctx context.Context, req Request) (map[string]any, error) {
		bc, err := newBecomussyClient(req, configPath)
		if err != nil {
			return nil, err
		}
		qs := becomussyQueryString(req.Args, []string{"project_id", "status", "overdue", "limit", "offset"})
		return bc.doGet(ctx, "/api/v1/commitments"+qs)
	}
}

func becomussyCommitmentsCreate(configPath string) Handler {
	return func(ctx context.Context, req Request) (map[string]any, error) {
		bc, err := newBecomussyClient(req, configPath)
		if err != nil {
			return nil, err
		}
		body := make(map[string]any)
		for _, key := range []string{"project_id", "commitment_text", "made_to", "due_date", "risk_if_missed"} {
			if v, ok := req.Args[key]; ok && v != nil {
				body[key] = v
			}
		}
		return bc.doPost(ctx, "/api/v1/commitments", body)
	}
}

func becomussyAuditList(configPath string) Handler {
	return func(ctx context.Context, req Request) (map[string]any, error) {
		bc, err := newBecomussyClient(req, configPath)
		if err != nil {
			return nil, err
		}
		qs := becomussyQueryString(req.Args, []string{"entity_type", "event_type", "actor", "limit", "offset"})
		return bc.doGet(ctx, "/api/v1/audit"+qs)
	}
}
