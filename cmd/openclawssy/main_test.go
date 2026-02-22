package main

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openclawssy/internal/channels/chat"
	"openclawssy/internal/channels/cli"
	"openclawssy/internal/channels/discord"
	httpchannel "openclawssy/internal/channels/http"
	"openclawssy/internal/chatstore"
	"openclawssy/internal/config"
	"openclawssy/internal/scheduler"
)

func TestChatAdaptersRouteBySource(t *testing.T) {
	store, err := chatstore.NewStore(filepath.Join(t.TempDir(), ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}

	sources := make([]string, 0, 2)
	thinkingModes := make([]string, 0, 2)
	connector := &chat.Connector{
		Store:          store,
		DefaultAgentID: "default",
		Queue: func(ctx context.Context, agentID, message, source, sessionID, thinkingMode string) (chat.QueuedRun, error) {
			_ = ctx
			_ = agentID
			_ = message
			if sessionID == "" {
				t.Fatal("expected session id")
			}
			sources = append(sources, source)
			thinkingModes = append(thinkingModes, thinkingMode)
			return chat.QueuedRun{ID: "run-1", Status: "queued"}, nil
		},
	}

	handler := buildDiscordMessageHandler(connector, "default")
	resp, err := handler(context.Background(), discord.Message{UserID: "u1", RoomID: "c1", Text: "hello", ThinkingMode: "always"})
	if err != nil {
		t.Fatalf("discord handler error: %v", err)
	}
	if resp.ID != "run-1" {
		t.Fatalf("unexpected discord run id: %q", resp.ID)
	}

	adapter := scopedChatAdapter{connector: connector, source: "dashboard", defaultAgentID: "default"}
	httpResp, err := adapter.HandleMessage(context.Background(), httpchannel.ChatMessage{UserID: "u1", RoomID: "dashboard", Message: "hello", ThinkingMode: "on_error"})
	if err != nil {
		t.Fatalf("dashboard adapter error: %v", err)
	}
	if httpResp.ID != "run-1" {
		t.Fatalf("unexpected dashboard run id: %q", httpResp.ID)
	}

	if len(sources) != 2 {
		t.Fatalf("expected 2 queued calls, got %d", len(sources))
	}
	if sources[0] != "discord" || sources[1] != "dashboard" {
		t.Fatalf("unexpected source routing: %#v", sources)
	}
	if thinkingModes[0] != "always" || thinkingModes[1] != "on_error" {
		t.Fatalf("unexpected thinking mode routing: %#v", thinkingModes)
	}
}

func TestCronServiceSupportsDeleteAndPauseResume(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	defer func() {
		_ = os.Chdir(wd)
	}()

	svc := cronService{}
	if _, err := svc.Cron(context.Background(), cli.CronInput{Command: "add", Args: []string{"-id", "job-1", "-schedule", "@every 1m", "-message", "ping"}}); err != nil {
		t.Fatalf("add job: %v", err)
	}
	if _, err := svc.Cron(context.Background(), cli.CronInput{Command: "pause"}); err != nil {
		t.Fatalf("pause scheduler: %v", err)
	}
	out, err := svc.Cron(context.Background(), cli.CronInput{Command: "list"})
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if !strings.Contains(out, "scheduler=paused") {
		t.Fatalf("expected paused scheduler state, got %q", out)
	}
	if _, err := svc.Cron(context.Background(), cli.CronInput{Command: "resume", Args: []string{"-id", "job-1"}}); err != nil {
		t.Fatalf("resume job: %v", err)
	}
	if _, err := svc.Cron(context.Background(), cli.CronInput{Command: "delete", Args: []string{"-id", "job-1"}}); err != nil {
		t.Fatalf("delete job via alias: %v", err)
	}
	out, err = svc.Cron(context.Background(), cli.CronInput{Command: "list"})
	if err != nil {
		t.Fatalf("list jobs after delete: %v", err)
	}
	if !strings.Contains(out, "no jobs") {
		t.Fatalf("expected no jobs output, got %q", out)
	}
}

func TestScopedChatAdapterRateLimitIncludesCooldown(t *testing.T) {
	store, err := chatstore.NewStore(filepath.Join(t.TempDir(), ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	now := time.Date(2026, 2, 17, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	connector := &chat.Connector{
		Store:          store,
		DefaultAgentID: "default",
		GlobalLimiter:  chat.NewRateLimiterWithClock(1, time.Minute, clock),
		Queue: func(ctx context.Context, agentID, message, source, sessionID, thinkingMode string) (chat.QueuedRun, error) {
			_ = ctx
			_ = agentID
			_ = message
			_ = source
			_ = sessionID
			_ = thinkingMode
			return chat.QueuedRun{ID: "run-1", Status: "queued"}, nil
		},
	}
	adapter := scopedChatAdapter{
		connector:      connector,
		source:         "dashboard",
		defaultAgentID: "default",
	}

	if _, err := adapter.HandleMessage(context.Background(), httpchannel.ChatMessage{UserID: "u1", RoomID: "dashboard", Message: "hello"}); err != nil {
		t.Fatalf("first message should pass: %v", err)
	}
	_, err = adapter.HandleMessage(context.Background(), httpchannel.ChatMessage{UserID: "u2", RoomID: "dashboard", Message: "hello"})
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	if !errors.Is(err, chat.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
	var rateErr *chat.RateLimitError
	if !errors.As(err, &rateErr) {
		t.Fatalf("expected RateLimitError, got %T", err)
	}
	if rateErr.RetryAfterSeconds < 1 {
		t.Fatalf("expected cooldown seconds, got %+v", rateErr)
	}
}

func TestResolveScheduledJobSessionUsesActivePointer(t *testing.T) {
	store, err := chatstore.NewStore(filepath.Join(t.TempDir(), ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}
	session, err := store.CreateSession(chatstore.CreateSessionInput{AgentID: "default", Channel: "dashboard", UserID: "dashboard_user", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.SetActiveSessionPointer("default", "dashboard", "dashboard_user", "dashboard", session.SessionID); err != nil {
		t.Fatalf("set active pointer: %v", err)
	}

	resolved, err := resolveScheduledJobSession(store, scheduler.Job{AgentID: "default", Channel: "dashboard", UserID: "dashboard_user", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("resolve session: %v", err)
	}
	if resolved != session.SessionID {
		t.Fatalf("expected existing active session %q, got %q", session.SessionID, resolved)
	}
}

func TestResolveScheduledJobSessionCreatesSessionWhenMissing(t *testing.T) {
	store, err := chatstore.NewStore(filepath.Join(t.TempDir(), ".openclawssy", "agents"))
	if err != nil {
		t.Fatalf("new chat store: %v", err)
	}

	resolved, err := resolveScheduledJobSession(store, scheduler.Job{AgentID: "default", Channel: "dashboard", UserID: "dashboard_user", RoomID: "dashboard"})
	if err != nil {
		t.Fatalf("resolve session: %v", err)
	}
	if strings.TrimSpace(resolved) == "" {
		t.Fatal("expected created session id")
	}
	session, err := store.GetSession(resolved)
	if err != nil {
		t.Fatalf("get created session: %v", err)
	}
	if session.Channel != "dashboard" || session.UserID != "dashboard_user" || session.RoomID != "dashboard" {
		t.Fatalf("unexpected created session metadata: %+v", session)
	}
}

func TestEnsureDefaultMemoryCheckpointJob(t *testing.T) {
	store, err := scheduler.NewStore(filepath.Join(t.TempDir(), "jobs.json"))
	if err != nil {
		t.Fatalf("new scheduler store: %v", err)
	}
	cfg := config.Default()
	cfg.Memory.Enabled = true
	cfg.Memory.AutoCheckpoint = true

	if err := ensureDefaultMemoryCheckpointJob(cfg, store); err != nil {
		t.Fatalf("ensure default checkpoint job: %v", err)
	}
	jobs := store.List()
	if len(jobs) != 1 {
		t.Fatalf("expected one scheduler job, got %d", len(jobs))
	}
	job := jobs[0]
	if job.ID != "memory-checkpoint-default" {
		t.Fatalf("unexpected job id %q", job.ID)
	}
	if job.Schedule != "@every 6h" {
		t.Fatalf("unexpected schedule %q", job.Schedule)
	}
	if job.Message != "/tool memory.checkpoint {}" {
		t.Fatalf("unexpected message %q", job.Message)
	}

	if err := ensureDefaultMemoryCheckpointJob(cfg, store); err != nil {
		t.Fatalf("ensure idempotent checkpoint job: %v", err)
	}
	if len(store.List()) != 1 {
		t.Fatalf("expected idempotent setup to keep one job, got %d", len(store.List()))
	}
}

func TestEnsureDefaultMemoryMaintenanceJob(t *testing.T) {
	store, err := scheduler.NewStore(filepath.Join(t.TempDir(), "jobs.json"))
	if err != nil {
		t.Fatalf("new scheduler store: %v", err)
	}
	cfg := config.Default()
	cfg.Memory.Enabled = true

	if err := ensureDefaultMemoryMaintenanceJob(cfg, store); err != nil {
		t.Fatalf("ensure default maintenance job: %v", err)
	}
	jobs := store.List()
	if len(jobs) != 1 {
		t.Fatalf("expected one scheduler job, got %d", len(jobs))
	}
	job := jobs[0]
	if job.ID != "memory-maintenance-default" {
		t.Fatalf("unexpected job id %q", job.ID)
	}
	if job.Schedule != "@every 168h" {
		t.Fatalf("unexpected schedule %q", job.Schedule)
	}
	if job.Message != "/tool memory.maintenance {}" {
		t.Fatalf("unexpected message %q", job.Message)
	}

	if err := ensureDefaultMemoryMaintenanceJob(cfg, store); err != nil {
		t.Fatalf("ensure idempotent maintenance job: %v", err)
	}
	if len(store.List()) != 1 {
		t.Fatalf("expected idempotent setup to keep one job, got %d", len(store.List()))
	}
}

func TestHandleSetupRejectsUnknownArgs(t *testing.T) {
	if code := handleSetup([]string{"--definitely-not-a-real-flag"}); code != 2 {
		t.Fatalf("expected setup parse failure exit code 2, got %d", code)
	}
}

func TestEnsureSelfSignedIncludesLoopbackIPs(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")

	if err := ensureSelfSigned(certPath, keyPath); err != nil {
		t.Fatalf("ensure self-signed cert: %v", err)
	}

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("failed to decode cert pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}

	if len(cert.IPAddresses) == 0 {
		t.Fatalf("expected loopback IP SANs, got none")
	}
	foundV4 := false
	foundV6 := false
	for _, ip := range cert.IPAddresses {
		if ip.String() == "127.0.0.1" {
			foundV4 = true
		}
		if ip.String() == "::1" {
			foundV6 = true
		}
	}
	if !foundV4 || !foundV6 {
		t.Fatalf("expected 127.0.0.1 and ::1 SANs, got %+v", cert.IPAddresses)
	}
}
