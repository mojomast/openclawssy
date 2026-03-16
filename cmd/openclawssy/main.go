package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"openclawssy/internal/agent"
	"openclawssy/internal/channels/chat"
	"openclawssy/internal/channels/cli"
	"openclawssy/internal/channels/dashboard"
	"openclawssy/internal/channels/discord"
	httpchannel "openclawssy/internal/channels/http"
	"openclawssy/internal/channels/telegram"
	"openclawssy/internal/chatstore"
	"openclawssy/internal/config"
	"openclawssy/internal/logger"
	"openclawssy/internal/messagecontent"
	"openclawssy/internal/runtime"
	"openclawssy/internal/scheduler"
	"openclawssy/internal/secrets"
)

const (
	envSandboxActive                 = "OPENCLAWSSY_SANDBOX_ACTIVE"
	envSandboxDockerHardened         = "OPENCLAWSSY_SANDBOX_DOCKER_HARDENED"
	envSandboxRequireDedicatedDaemon = "OPENCLAWSSY_SANDBOX_DOCKER_REQUIRE_DEDICATED_DAEMON"
	envSandboxDockerHost             = "OPENCLAWSSY_SANDBOX_DOCKER_HOST"
	remoteAuthTokenSecretKey         = "openclaw/remote/auth_token"
)

func main() {
	logger.Init()
	ctx := context.Background()
	engine, err := runtime.NewEngine(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	handlers := cli.Handlers{Init: initService{engine: engine}, Ask: askService{engine: engine}, Run: runService{engine: engine}, Doctor: doctorService{}, Cron: cronService{}, Out: os.Stdout, Err: os.Stderr}
	evalSvc := evalService{engine: engine, out: os.Stdout, err: os.Stderr}

	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
	}

	var code int
	switch os.Args[1] {
	case "init":
		code = handlers.HandleInit(ctx, os.Args[2:])
	case "setup":
		code = handleSetup(os.Args[2:])
	case "ask":
		code = handlers.HandleAsk(ctx, os.Args[2:])
	case "run":
		code = handlers.HandleRun(ctx, os.Args[2:])
	case "doctor":
		code = handlers.HandleDoctor(ctx, os.Args[2:])
	case "cron":
		code = handlers.HandleCron(ctx, os.Args[2:])
	case "serve":
		code = handleServe(ctx, engine, os.Args[2:])
	case "remote":
		code = handleRemote(ctx, os.Args[2:])
	case "openclaw":
		code = handleOpenClaw(ctx, os.Args[2:])
	case "eval":
		code = evalSvc.HandleEval(ctx, os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n", os.Args[1])
		printUsage(os.Stderr)
		code = 2
	}

	os.Exit(code)
}

func printUsage(w *os.File) {
	fmt.Fprintln(w, "usage: openclawssy <subcommand> [flags]")
	fmt.Fprintln(w, "subcommands: init, setup, ask, run, serve, cron, doctor, remote, openclaw, eval")
}

func handleOpenClaw(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: openclawssy openclaw remote <pull|status|send|history|reconnect> [flags]")
		return 2
	}
	if strings.EqualFold(strings.TrimSpace(args[0]), "remote") {
		return handleRemote(ctx, args[1:])
	}
	fmt.Fprintf(os.Stderr, "unsupported openclaw command: %s\n", args[0])
	return 2
}

func handleRemote(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: openclawssy remote <pull|status|send|history|reconnect> [flags]")
		return 2
	}
	command := strings.ToLower(strings.TrimSpace(args[0]))
	cmdArgs := args[1:]
	cfg, err := config.LoadOrDefault(filepath.Join(".openclawssy", "config.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	debug := false

	switch command {
	case "pull":
		return handleRemotePull(ctx, cfg, cmdArgs)
	case "status":
		fs := flag.NewFlagSet("remote status", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		fs.BoolVar(&debug, "debug", false, "include sanitized websocket frame artifacts")
		if err := fs.Parse(cmdArgs); err != nil {
			return 2
		}
		if !cfg.OpenClaw.Remote.Enabled {
			fmt.Fprintln(os.Stderr, "openclaw.remote.enabled is false; enable it in .openclawssy/config.json")
			return 1
		}
		token, tokenErr := loadRemoteAuthToken(cfg)
		if tokenErr != nil {
			fmt.Fprintln(os.Stderr, tokenErr)
			return 1
		}
		if err := runRemoteBridge(ctx, cfg, token, debug, []string{"status"}, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	case "send":
		fs := flag.NewFlagSet("remote send", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		fs.BoolVar(&debug, "debug", false, "include sanitized websocket frame artifacts")
		if err := fs.Parse(cmdArgs); err != nil {
			return 2
		}
		message := strings.TrimSpace(strings.Join(fs.Args(), " "))
		if message == "" {
			fmt.Fprintln(os.Stderr, "usage: openclawssy remote send [--debug] \"<message>\"")
			return 2
		}
		if !cfg.OpenClaw.Remote.Enabled {
			fmt.Fprintln(os.Stderr, "openclaw.remote.enabled is false; enable it in .openclawssy/config.json")
			return 1
		}
		token, tokenErr := loadRemoteAuthToken(cfg)
		if tokenErr != nil {
			fmt.Fprintln(os.Stderr, tokenErr)
			return 1
		}
		if err := runRemoteBridge(ctx, cfg, token, debug, []string{"send", "--message", message}, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	case "history":
		fs := flag.NewFlagSet("remote history", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		limit := 20
		fs.IntVar(&limit, "limit", 20, "maximum history messages")
		fs.BoolVar(&debug, "debug", false, "include sanitized websocket frame artifacts")
		if err := fs.Parse(cmdArgs); err != nil {
			return 2
		}
		if !cfg.OpenClaw.Remote.Enabled {
			fmt.Fprintln(os.Stderr, "openclaw.remote.enabled is false; enable it in .openclawssy/config.json")
			return 1
		}
		token, tokenErr := loadRemoteAuthToken(cfg)
		if tokenErr != nil {
			fmt.Fprintln(os.Stderr, tokenErr)
			return 1
		}
		if err := runRemoteBridge(ctx, cfg, token, debug, []string{"history", "--limit", fmt.Sprintf("%d", limit)}, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	case "reconnect":
		fs := flag.NewFlagSet("remote reconnect", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		fs.BoolVar(&debug, "debug", false, "include sanitized websocket frame artifacts")
		if err := fs.Parse(cmdArgs); err != nil {
			return 2
		}
		if !cfg.OpenClaw.Remote.Enabled {
			fmt.Fprintln(os.Stderr, "openclaw.remote.enabled is false; enable it in .openclawssy/config.json")
			return 1
		}
		token, tokenErr := loadRemoteAuthToken(cfg)
		if tokenErr != nil {
			fmt.Fprintln(os.Stderr, tokenErr)
			return 1
		}
		if err := runRemoteBridge(ctx, cfg, token, debug, []string{"reconnect"}, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unsupported remote command: %s\n", command)
		return 2
	}
}

func handleRemotePull(ctx context.Context, cfg config.Config, args []string) int {
	fs := flag.NewFlagSet("remote pull", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	targetDir := filepath.Join(".openclawssy", "external", "openclawremoteussy")
	repoURL := strings.TrimSpace(cfg.OpenClaw.Remote.RepositoryURL)
	if repoURL == "" {
		repoURL = "https://github.com/mojomast/openclawremoteussy.git"
	}
	fs.StringVar(&targetDir, "dir", targetDir, "target clone directory")
	fs.StringVar(&repoURL, "repo", repoURL, "openclawremoteussy repository URL")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	gitDir := filepath.Join(targetDir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		if runErr := runGitCommand(ctx, "", []string{"-C", targetDir, "pull", "--ff-only"}, os.Stdout, os.Stderr); runErr != nil {
			fmt.Fprintln(os.Stderr, runErr)
			return 1
		}
		fmt.Fprintf(os.Stdout, "updated openclawremoteussy at %s\n", targetDir)
	} else {
		if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if runErr := runGitCommand(ctx, "", []string{"clone", repoURL, targetDir}, os.Stdout, os.Stderr); runErr != nil {
			fmt.Fprintln(os.Stderr, runErr)
			return 1
		}
		fmt.Fprintf(os.Stdout, "cloned openclawremoteussy to %s\n", targetDir)
	}

	fmt.Fprintln(os.Stdout, "next steps:")
	fmt.Fprintf(os.Stdout, "1) go -C %s build ./cmd/openclawremoteussy\n", targetDir)
	fmt.Fprintf(os.Stdout, "2) set openclaw.remote.binary_path in .openclawssy/config.json to %s/openclawremoteussy\n", filepath.ToSlash(targetDir))
	fmt.Fprintln(os.Stdout, "3) set secret key openclaw/remote/auth_token, then run: openclawssy remote status")
	return 0
}

func loadRemoteAuthToken(cfg config.Config) (string, error) {
	store, err := secrets.NewStore(cfg)
	if err != nil {
		return "", err
	}
	value, ok, err := store.Get(remoteAuthTokenSecretKey)
	if err != nil {
		return "", err
	}
	if !ok || strings.TrimSpace(value) == "" {
		return "", errors.New("missing secret openclaw/remote/auth_token; set it before using remote commands")
	}
	return strings.TrimSpace(value), nil
}

func runRemoteBridge(ctx context.Context, cfg config.Config, authToken string, debug bool, commandArgs []string, stdout, stderr io.Writer) error {
	binaryPath := strings.TrimSpace(cfg.OpenClaw.Remote.BinaryPath)
	if binaryPath == "" {
		binaryPath = "openclawremoteussy"
	}
	baseArgs := []string{
		"--ws-primary", cfg.OpenClaw.Remote.WSPrimary,
		"--session-key", cfg.OpenClaw.Remote.SessionKey,
		"--connect-timeout-ms", fmt.Sprintf("%d", cfg.OpenClaw.Remote.ConnectTimeoutMS),
		"--request-timeout-ms", fmt.Sprintf("%d", cfg.OpenClaw.Remote.RequestTimeoutMS),
		"--poll-interval-ms", fmt.Sprintf("%d", cfg.OpenClaw.Remote.PollIntervalMS),
		"--poll-timeout-ms", fmt.Sprintf("%d", cfg.OpenClaw.Remote.PollTimeoutMS),
		"--prefer-tailnet-wss", fmt.Sprintf("%t", cfg.OpenClaw.Remote.PreferTailnetWSS),
		"--state-file", filepath.Join(".openclawssy", "openclaw", "remote_state.json"),
	}
	if strings.TrimSpace(cfg.OpenClaw.Remote.WSFallback) != "" {
		baseArgs = append(baseArgs, "--ws-fallback", cfg.OpenClaw.Remote.WSFallback)
	}
	if debug {
		baseArgs = append(baseArgs, "--debug")
	}
	args := append(baseArgs, commandArgs...)
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Env = append(os.Environ(), "OPENCLAWREMOTEUSSY_AUTH_TOKEN="+authToken)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("openclawremoteussy command failed: %w", err)
	}
	return nil
}

func probeRemoteBridge(ctx context.Context, cfg config.Config, secretStore *secrets.Store) {
	if !cfg.OpenClaw.Remote.Enabled {
		return
	}
	if secretStore == nil {
		fmt.Fprintln(os.Stderr, "openclaw remote integration warning: secret store unavailable")
		return
	}
	token, ok, err := secretStore.Get(remoteAuthTokenSecretKey)
	if err != nil || !ok || strings.TrimSpace(token) == "" {
		fmt.Fprintln(os.Stderr, "openclaw remote integration warning: missing openclaw/remote/auth_token")
		return
	}
	var stderr bytes.Buffer
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	err = runRemoteBridge(probeCtx, cfg, strings.TrimSpace(token), false, []string{"status", "--healthcheck"}, io.Discard, &stderr)
	cancel()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			fmt.Fprintf(os.Stderr, "openclaw remote startup warning: %v (%s)\n", err, message)
			return
		}
		fmt.Fprintln(os.Stderr, "openclaw remote startup warning:", err)
	}
}

func runGitCommand(ctx context.Context, workdir string, args []string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	if strings.TrimSpace(workdir) != "" {
		cmd.Dir = workdir
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git command failed: %w", err)
	}
	return nil
}

func handleServe(ctx context.Context, engine *runtime.Engine, args []string) int {
	serveCfg, err := cli.ParseServeArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	runStore, err := httpchannel.NewFileRunStore(serveCfg.RunsFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	eventBus := httpchannel.NewRunEventBus(0)
	runTracker := httpchannel.NewActiveRunTracker()

	exec := runtimeExecutor{engine: engine}
	runtimeCfg, err := config.LoadOrDefault(filepath.Join(".openclawssy", "config.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	// Apply CLI overrides for sandbox settings.
	if serveCfg.SandboxActive {
		runtimeCfg.Sandbox.Active = true
	}
	if serveCfg.SandboxProvider != "" {
		runtimeCfg.Sandbox.Provider = serveCfg.SandboxProvider
	}
	// When sandbox is active but provider is still "none", default to docker.
	if runtimeCfg.Sandbox.Active && runtimeCfg.Sandbox.Provider == "none" {
		runtimeCfg.Sandbox.Provider = "docker"
	}
	runtimeCfg.ApplyDefaults()
	if err := runtimeCfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "config validation:", err)
		return 1
	}

	secretStore, secretErr := secrets.NewStore(runtimeCfg)
	if secretErr == nil {
		if token, ok, _ := secretStore.Get("discord/bot_token"); ok && strings.TrimSpace(token) != "" {
			runtimeCfg.Discord.Token = token
		}
		if token, ok, _ := secretStore.Get("telegram/bot_token"); ok && strings.TrimSpace(token) != "" {
			runtimeCfg.Telegram.Token = token
		}
	}

	probeRemoteBridge(ctx, runtimeCfg, secretStore)

	jobsStore, err := scheduler.NewStore(serveCfg.JobsFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := ensureDefaultMemoryCheckpointJob(runtimeCfg, jobsStore); err != nil {
		fmt.Fprintln(os.Stderr, "scheduler setup warning:", err)
	}
	if err := ensureDefaultMemoryMaintenanceJob(runtimeCfg, jobsStore); err != nil {
		fmt.Fprintln(os.Stderr, "scheduler setup warning:", err)
	}
	var schedulerChatStore *chatstore.Store
	if runtimeCfg.Chat.Enabled || runtimeCfg.Discord.Enabled || runtimeCfg.Telegram.Enabled {
		schedulerChatStore, err = chatstore.NewStore(filepath.Join(".openclawssy", "agents"))
		if err != nil {
			fmt.Fprintln(os.Stderr, "failed to initialize scheduler chat delivery:", err)
			return 1
		}
	}
	schedulerExec := scheduler.NewExecutorWithJobPolicy(jobsStore, time.Second, runtimeCfg.Scheduler.MaxConcurrentJobs, runtimeCfg.Scheduler.CatchUp, func(job scheduler.Job) {
		agentID := strings.TrimSpace(job.AgentID)
		if agentID == "" {
			agentID = "default"
		}
		sessionID, err := resolveScheduledJobSession(schedulerChatStore, job)
		if err != nil {
			fmt.Fprintln(os.Stderr, "scheduler delivery warning:", err)
		}
		source := "scheduler"
		if channel := strings.TrimSpace(job.Channel); channel != "" {
			source = "scheduler/" + channel
		}
		if _, err := httpchannel.QueueRunWithOptions(
			context.Background(),
			runStore,
			exec,
			agentID,
			job.Message,
			nil,
			source,
			sessionID,
			"",
			httpchannel.QueueRunOptions{EventBus: eventBus, Tracker: runTracker},
		); err != nil {
			fmt.Fprintln(os.Stderr, "scheduler queue warning:", err)
		}
	})
	schedulerExec.Start()
	defer schedulerExec.Stop()

	sharedChat, err := buildSharedChatConnector(runtimeCfg, runStore, exec, eventBus, runTracker)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	var dBot *discord.Bot
	if runtimeCfg.Discord.Enabled {
		dBot, err = discord.New(
			runtimeCfg,
			buildDiscordMessageHandler(sharedChat, runtimeCfg.Discord.DefaultAgentID),
			func(ctx context.Context, runID string) (discord.RunStatus, error) {
				run, err := runStore.Get(ctx, runID)
				if err != nil {
					return discord.RunStatus{}, err
				}
				return discord.RunStatus{Status: run.Status, Output: run.Output, Error: run.Error, ArtifactPath: run.ArtifactPath, Trace: run.Trace}, nil
			},
		)
		if err != nil {
			fmt.Fprintln(os.Stderr, "discord disabled:", err)
		} else {
			dBot.SetOutcomeResponder(buildDiscordOutcomeResponder(runtimeCfg, secretStore))
			if err := dBot.Start(); err != nil {
				fmt.Fprintln(os.Stderr, "discord start failed:", err)
			} else {
				defer dBot.Stop()
			}
		}
	}

	var tBot *telegram.Bot
	if runtimeCfg.Telegram.Enabled {
		tBot, err = telegram.New(
			runtimeCfg,
			buildTelegramMessageHandler(sharedChat, runtimeCfg.Telegram.DefaultAgentID),
			func(ctx context.Context, runID string) (telegram.RunStatus, error) {
				run, err := runStore.Get(ctx, runID)
				if err != nil {
					return telegram.RunStatus{}, err
				}
				return telegram.RunStatus{Status: run.Status, Output: run.Output, Error: run.Error, ArtifactPath: run.ArtifactPath, Trace: run.Trace}, nil
			},
		)
		if err != nil {
			fmt.Fprintln(os.Stderr, "telegram disabled:", err)
		} else {
			tBot.SetOutcomeResponder(buildTelegramOutcomeResponder(runtimeCfg, secretStore))
			if err := tBot.Start(); err != nil {
				fmt.Fprintln(os.Stderr, "telegram start failed:", err)
			} else {
				defer tBot.Stop()
			}
		}
	}

	dash := dashboard.NewWithOptions(".", runStore, dashboard.Options{
		SchedulerStore: jobsStore,
		RunCanceller: runCancelCoordinator{cancellers: []runCanceller{
			engine.RunTracker(),
			runTracker,
		}},
		EffectiveConfig: &runtimeCfg,
	})
	server := httpchannel.NewServer(httpchannel.Config{
		Addr:        serveCfg.Addr,
		BearerToken: serveCfg.Token,
		Store:       runStore,
		Executor:    exec,
		RunTracker:  runTracker,
		Chat:        buildDashboardChatConnector(runtimeCfg, sharedChat),
		EventBus:    eventBus,
		RegisterMux: func(mux *http.ServeMux) {
			if runtimeCfg.Server.Dashboard {
				dash.Register(mux)
			}
			// Wire sandbox admin endpoints under /api/admin/sandbox/docker/…
			// These are protected by the existing authMiddleware via the mux.
			sandboxAdmin := httpchannel.NewSandboxAdminHandler(httpchannel.NewDockerAdminManager())
			sandboxAdmin.Register(mux)
		},
	})

	if runtimeCfg.Server.TLSEnabled {
		if err := server.ListenAndServeTLS(ctx, runtimeCfg.Server.TLSCertFile, runtimeCfg.Server.TLSKeyFile); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}

	if err := server.ListenAndServe(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	return 0
}

func ensureDefaultMemoryCheckpointJob(cfg config.Config, jobsStore *scheduler.Store) error {
	if jobsStore == nil {
		return nil
	}
	if !cfg.Memory.Enabled || !cfg.Memory.AutoCheckpoint {
		return nil
	}
	const defaultCheckpointJobID = "memory-checkpoint-default"
	for _, job := range jobsStore.List() {
		if strings.TrimSpace(job.ID) == defaultCheckpointJobID {
			return nil
		}
	}
	return jobsStore.Add(scheduler.Job{
		ID:       defaultCheckpointJobID,
		AgentID:  "default",
		Schedule: "@every 6h",
		Message:  "/tool memory.checkpoint {}",
		Channel:  "system",
		UserID:   "system",
		RoomID:   "maintenance",
		Enabled:  true,
	})
}

func ensureDefaultMemoryMaintenanceJob(cfg config.Config, jobsStore *scheduler.Store) error {
	if jobsStore == nil {
		return nil
	}
	if !cfg.Memory.Enabled {
		return nil
	}
	const defaultMaintenanceJobID = "memory-maintenance-default"
	for _, job := range jobsStore.List() {
		if strings.TrimSpace(job.ID) == defaultMaintenanceJobID {
			return nil
		}
	}
	return jobsStore.Add(scheduler.Job{
		ID:       defaultMaintenanceJobID,
		AgentID:  "default",
		Schedule: "@every 168h",
		Message:  "/tool memory.maintenance {}",
		Channel:  "system",
		UserID:   "system",
		RoomID:   "maintenance",
		Enabled:  true,
	})
}

func resolveScheduledJobSession(store *chatstore.Store, job scheduler.Job) (string, error) {
	if store == nil {
		return "", nil
	}
	agentID := strings.TrimSpace(job.AgentID)
	if agentID == "" {
		agentID = "default"
	}
	channel := strings.TrimSpace(job.Channel)
	if channel == "" {
		channel = "dashboard"
	}
	userID := strings.TrimSpace(job.UserID)
	if userID == "" {
		userID = "dashboard_user"
	}
	roomID := strings.TrimSpace(job.RoomID)
	if roomID == "" {
		roomID = "dashboard"
	}

	if sessionID := strings.TrimSpace(job.SessionID); sessionID != "" {
		session, err := store.GetSession(sessionID)
		if err == nil {
			if !session.IsClosed() && session.AgentID == agentID && session.Channel == channel && session.UserID == userID && session.RoomID == roomID {
				return sessionID, nil
			}
		}
	}

	sessionID, err := store.GetActiveSessionPointer(agentID, channel, userID, roomID)
	if err == nil {
		return sessionID, nil
	}
	if !errors.Is(err, chatstore.ErrSessionNotFound) {
		return "", err
	}

	session, err := store.CreateSession(chatstore.CreateSessionInput{AgentID: agentID, Channel: channel, UserID: userID, RoomID: roomID})
	if err != nil {
		return "", err
	}
	if err := store.SetActiveSessionPointer(agentID, channel, userID, roomID, session.SessionID); err != nil {
		return "", err
	}
	return session.SessionID, nil
}

type initService struct{ engine *runtime.Engine }

func (s initService) Init(_ context.Context, input cli.InitInput) error {
	if s.engine == nil {
		return errors.New("runtime engine is not configured")
	}
	eng := s.engine
	if input.Workspace != "" && input.Workspace != "." {
		custom, err := runtime.NewEngine(input.Workspace)
		if err != nil {
			return err
		}
		eng = custom
	}
	if err := eng.Init(input.AgentID, input.Force); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "initialized agent=%q workspace=%q force=%t\n", input.AgentID, input.Workspace, input.Force)
	return nil
}

type askService struct{ engine *runtime.Engine }

func (s askService) Ask(ctx context.Context, input cli.AskInput) (string, error) {
	if s.engine == nil {
		return "", errors.New("runtime engine is not configured")
	}
	res, err := s.engine.ExecuteWithInput(ctx, runtime.ExecuteInput{AgentID: input.AgentID, Message: input.Message, ThinkingMode: input.ThinkingMode})
	if err != nil {
		return "", err
	}
	return res.FinalText, nil
}

type runService struct{ engine *runtime.Engine }

func (s runService) Run(ctx context.Context, input cli.RunInput) (string, error) {
	if s.engine == nil {
		return "", errors.New("runtime engine is not configured")
	}
	message := input.Message
	if message == "" && input.MessageFile != "" {
		b, err := os.ReadFile(input.MessageFile)
		if err != nil {
			return "", err
		}
		message = strings.TrimSpace(string(b))
	}
	if strings.TrimSpace(message) == "" {
		return "", errors.New("message is empty")
	}
	res, err := s.engine.Execute(ctx, input.AgentID, message)
	if err != nil {
		return "", err
	}
	if input.Detached {
		return fmt.Sprintf("run %s accepted", res.RunID), nil
	}
	return fmt.Sprintf("run %s completed\nartifacts: %s\n%s", res.RunID, res.ArtifactPath, res.FinalText), nil
}

type doctorService struct{}

func (doctorService) Doctor(_ context.Context, input cli.DoctorInput) (string, error) {
	workspace := "workspace"
	_, wsErr := os.Stat(workspace)
	state := "missing"
	if wsErr == nil {
		state = "ok"
	}

	cfg, cfgErr := config.LoadOrDefault(filepath.Join(".openclawssy", "config.json"))
	providerState := "not configured"
	secretState := "missing"
	if cfgErr == nil {
		endpoint, err := providerForDoctor(cfg)
		if err == nil {
			apiKey := endpoint.APIKey
			if apiKey == "" && endpoint.APIKeyEnv != "" {
				apiKey = os.Getenv(endpoint.APIKeyEnv)
			}
			if apiKey != "" {
				providerState = fmt.Sprintf("%s/%s key=env", cfg.Model.Provider, cfg.Model.Name)
			} else {
				store, serr := secrets.NewStore(cfg)
				if serr == nil {
					if v, ok, _ := store.Get("provider/" + strings.ToLower(cfg.Model.Provider) + "/api_key"); ok && strings.TrimSpace(v) != "" {
						providerState = fmt.Sprintf("%s/%s key=secret-store", cfg.Model.Provider, cfg.Model.Name)
						secretState = "ok"
					} else {
						providerState = fmt.Sprintf("%s/%s key=missing (%s)", cfg.Model.Provider, cfg.Model.Name, endpoint.APIKeyEnv)
					}
				}
			}
		}
	}

	if input.Verbose {
		setup := []string{
			"1) openclawssy setup",
			"2) export OPENCLAWSSY_MASTER_KEY if not using local master key file",
			"3) store provider key via dashboard or wizard",
			"4) run `openclawssy serve --token <token>` and open https dashboard",
		}
		if cfgErr != nil {
			return fmt.Sprintf("doctor: workspace=%s (%s) model=%s secrets=%s\nsetup:\n- %s", workspace, state, providerState, secretState, strings.Join(setup, "\n- ")), nil
		}
		return fmt.Sprintf("doctor: workspace=%s (%s) model=%s secrets=%s", workspace, state, providerState, secretState), nil
	}
	return "doctor: ok", nil
}

type cronService struct{}

func (cronService) Cron(_ context.Context, input cli.CronInput) (string, error) {
	store, err := scheduler.NewStore(filepath.Join(".openclawssy", "scheduler", "jobs.json"))
	if err != nil {
		return "", err
	}

	switch strings.ToLower(strings.TrimSpace(input.Command)) {
	case "list":
		jobs := store.List()
		state := "running"
		if store.IsPaused() {
			state = "paused"
		}
		if len(jobs) == 0 {
			return "scheduler=" + state + " no jobs", nil
		}
		lines := make([]string, 0, len(jobs))
		lines = append(lines, "scheduler="+state)
		for _, job := range jobs {
			lines = append(lines, fmt.Sprintf("%s %s %q enabled=%t", job.ID, job.Schedule, job.Message, job.Enabled))
		}
		return strings.Join(lines, "\n"), nil
	case "add":
		fs := flag.NewFlagSet("cron add", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		id := ""
		agentID := "default"
		channel := "dashboard"
		userID := "dashboard_user"
		roomID := "dashboard"
		sessionID := ""
		schedule := ""
		message := ""
		enabled := true
		fs.StringVar(&id, "id", "", "job id")
		fs.StringVar(&agentID, "agent", "default", "agent id")
		fs.StringVar(&channel, "channel", "dashboard", "delivery channel")
		fs.StringVar(&userID, "user", "dashboard_user", "delivery user id")
		fs.StringVar(&roomID, "room", "dashboard", "delivery room id")
		fs.StringVar(&sessionID, "session", "", "delivery session id (optional)")
		fs.StringVar(&schedule, "schedule", "", "schedule (@every 1m, @hourly, 0 * * * *, or RFC3339)")
		fs.StringVar(&message, "message", "", "message")
		fs.BoolVar(&enabled, "enabled", true, "enable job")
		if err := fs.Parse(input.Args); err != nil {
			return "", err
		}
		if schedule == "" || message == "" {
			return "", errors.New("-schedule and -message are required")
		}
		if id == "" {
			id = fmt.Sprintf("job_%d", time.Now().UTC().UnixNano())
		}
		if err := store.Add(scheduler.Job{ID: id, Schedule: schedule, AgentID: agentID, Message: message, Channel: channel, UserID: userID, RoomID: roomID, SessionID: sessionID, Enabled: enabled}); err != nil {
			return "", err
		}
		return "added job " + id, nil
	case "remove", "delete":
		fs := flag.NewFlagSet("cron remove", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		id := ""
		fs.StringVar(&id, "id", "", "job id")
		if err := fs.Parse(input.Args); err != nil {
			return "", err
		}
		if id == "" {
			return "", errors.New("-id is required")
		}
		if err := store.Remove(id); err != nil {
			return "", err
		}
		return "removed job " + id, nil
	case "pause", "resume":
		fs := flag.NewFlagSet("cron pause/resume", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		id := ""
		fs.StringVar(&id, "id", "", "job id (optional)")
		if err := fs.Parse(input.Args); err != nil {
			return "", err
		}
		enable := strings.EqualFold(strings.TrimSpace(input.Command), "resume")
		if strings.TrimSpace(id) != "" {
			if err := store.SetJobEnabled(strings.TrimSpace(id), enable); err != nil {
				return "", err
			}
			if enable {
				return "resumed job " + strings.TrimSpace(id), nil
			}
			return "paused job " + strings.TrimSpace(id), nil
		}
		if err := store.SetPaused(!enable); err != nil {
			return "", err
		}
		if enable {
			return "resumed scheduler", nil
		}
		return "paused scheduler", nil
	default:
		return "", fmt.Errorf("unsupported cron command: %s", input.Command)
	}
}

type runtimeExecutor struct{ engine *runtime.Engine }

type runCanceller interface {
	Cancel(runID string) error
	IsTracked(runID string) bool
}

type runCancelCoordinator struct {
	cancellers []runCanceller
}

func (c runCancelCoordinator) IsTracked(runID string) bool {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return false
	}
	for _, canceller := range c.cancellers {
		if canceller != nil && canceller.IsTracked(runID) {
			return true
		}
	}
	return false
}

func (c runCancelCoordinator) Cancel(runID string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return runtime.ErrRunNotFound
	}
	var lastNotFound error
	for _, canceller := range c.cancellers {
		if canceller == nil {
			continue
		}
		err := canceller.Cancel(runID)
		if err == nil {
			return nil
		}
		if errors.Is(err, runtime.ErrRunNotFound) || errors.Is(err, httpchannel.ErrTrackedRunNotFound) {
			lastNotFound = err
			continue
		}
		return err
	}
	if lastNotFound != nil {
		return lastNotFound
	}
	return runtime.ErrRunNotFound
}

func (e runtimeExecutor) Execute(ctx context.Context, input httpchannel.ExecutionInput) (httpchannel.ExecutionResult, error) {
	res, err := e.engine.ExecuteWithInput(ctx, runtime.ExecuteInput{
		InstanceID:   input.InstanceID,
		RunID:        input.RunID,
		AgentID:      input.AgentID,
		Message:      input.Message,
		ContentParts: append([]messagecontent.Part(nil), input.ContentParts...),
		Source:       input.Source,
		SessionID:    input.SessionID,
		ThinkingMode: input.ThinkingMode,
		OnProgress:   input.OnProgress,
	})
	if err != nil {
		return httpchannel.ExecutionResult{InstanceID: res.InstanceID, AgentID: res.AgentID, ParentRunID: res.ParentRunID, Trace: res.Trace, Provider: res.Provider, Model: res.Model, ToolCalls: res.ToolCalls}, err
	}
	return httpchannel.ExecutionResult{InstanceID: res.InstanceID, AgentID: res.AgentID, ParentRunID: res.ParentRunID, Output: res.FinalText, ArtifactPath: res.ArtifactPath, DurationMS: res.DurationMS, ToolCalls: res.ToolCalls, Provider: res.Provider, Model: res.Model, Trace: res.Trace}, nil
}

func buildSharedChatConnector(cfg config.Config, store httpchannel.RunStore, exec httpchannel.RunExecutor, eventBus *httpchannel.RunEventBus, runTracker *httpchannel.ActiveRunTracker) (*chat.Connector, error) {
	if !cfg.Chat.Enabled && !cfg.Discord.Enabled && !cfg.Telegram.Enabled {
		return nil, nil
	}
	chatStore, err := chatstore.NewStore(filepath.Join(".openclawssy", "agents"))
	if err != nil {
		return nil, fmt.Errorf("create chat store: %w", err)
	}
	defaultAgentID := strings.TrimSpace(cfg.Chat.DefaultAgentID)
	if defaultAgentID == "" {
		defaultAgentID = strings.TrimSpace(cfg.Discord.DefaultAgentID)
	}
	if defaultAgentID == "" {
		defaultAgentID = strings.TrimSpace(cfg.Telegram.DefaultAgentID)
	}
	if defaultAgentID == "" {
		defaultAgentID = "default"
	}
	return &chat.Connector{
		DefaultAgentID: defaultAgentID,
		Store:          chatStore,
		HistoryLimit:   30,
		GlobalLimiter:  chat.NewRateLimiter(cfg.Chat.GlobalRateLimitPerMin, time.Minute),
		Queue: func(ctx context.Context, agentID, message string, contentParts []messagecontent.Part, source, sessionID, thinkingMode string) (chat.QueuedRun, error) {
			run, err := httpchannel.QueueRunWithOptions(
				ctx,
				store,
				exec,
				agentID,
				message,
				contentParts,
				source,
				sessionID,
				thinkingMode,
				httpchannel.QueueRunOptions{EventBus: eventBus, Tracker: runTracker},
			)
			if err != nil {
				return chat.QueuedRun{}, err
			}
			return chat.QueuedRun{ID: run.ID, Status: run.Status}, nil
		},
	}, nil
}

func buildDashboardChatConnector(cfg config.Config, connector *chat.Connector) httpchannel.ChatConnector {
	if !cfg.Chat.Enabled || connector == nil {
		return nil
	}
	allowUsers := append([]string(nil), cfg.Chat.AllowUsers...)
	if len(allowUsers) == 0 {
		allowUsers = []string{"dashboard_user"}
	}
	return scopedChatAdapter{
		connector:      connector,
		source:         "dashboard",
		defaultAgentID: cfg.Chat.DefaultAgentID,
		allow:          chat.NewAllowlist(allowUsers, cfg.Chat.AllowRooms),
		limiter:        chat.NewRateLimiter(cfg.Chat.RateLimitPerMin, time.Minute),
	}
}

func buildDiscordMessageHandler(connector *chat.Connector, defaultAgentID string) discord.MessageHandler {
	return func(ctx context.Context, msg discord.Message) (discord.Response, error) {
		queued, err := queueChannelMessage(ctx, connector, defaultAgentID, "discord", msg.UserID, msg.RoomID, msg.AgentID, msg.Text, nil, msg.ThinkingMode)
		if err != nil {
			return discord.Response{}, err
		}
		return discord.Response{ID: queued.ID, Status: queued.Status, Response: queued.Response}, nil
	}
}

func buildTelegramMessageHandler(connector *chat.Connector, defaultAgentID string) telegram.MessageHandler {
	return func(ctx context.Context, msg telegram.Message) (telegram.Response, error) {
		queued, err := queueChannelMessage(ctx, connector, defaultAgentID, "telegram", msg.UserID, msg.RoomID, msg.AgentID, msg.Text, msg.ContentParts, msg.ThinkingMode)
		if err != nil {
			return telegram.Response{}, err
		}
		return telegram.Response{ID: queued.ID, Status: queued.Status, Response: queued.Response}, nil
	}
}

func buildDiscordOutcomeResponder(cfg config.Config, secretStore *secrets.Store) discord.OutcomeResponder {
	model, err := runtime.NewProviderModel(cfg, buildSecretLookup(secretStore))
	if err != nil {
		return nil
	}
	return func(ctx context.Context, input discord.OutcomeInput) (string, error) {
		return generateBridgeOutcomeReply(ctx, model, "discord", input.Status, input.Output, input.Error, input.ArtifactPath, input.ToolSummary)
	}
}

func buildTelegramOutcomeResponder(cfg config.Config, secretStore *secrets.Store) telegram.OutcomeResponder {
	model, err := runtime.NewProviderModel(cfg, buildSecretLookup(secretStore))
	if err != nil {
		return nil
	}
	return func(ctx context.Context, input telegram.OutcomeInput) (string, error) {
		return generateBridgeOutcomeReply(ctx, model, "telegram", input.Status, input.Output, input.Error, input.ArtifactPath, input.ToolSummary)
	}
}

func buildSecretLookup(store *secrets.Store) runtime.SecretLookup {
	if store == nil {
		return nil
	}
	return func(name string) (string, bool, error) {
		return store.Get(name)
	}
}

func generateBridgeOutcomeReply(ctx context.Context, model *runtime.ProviderModel, channel, status, output, errText, artifactPath, toolSummary string) (string, error) {
	if model == nil {
		return "", errors.New("model is not configured")
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = "unknown"
	}
	prompt := strings.Join([]string{
		"Write a short, user-facing follow-up message for a chat bridge.",
		"",
		"Rules:",
		"- Use plain text only.",
		"- Keep it to 1-2 sentences.",
		"- Be empathetic and action-oriented.",
		"- Do not mention run IDs, tool names, traces, stack traces, config keys, or internal system details.",
		"- Respond directly; do not request tool calls.",
		"- If status is timeout or status_lookup_error, acknowledge delay and ask the user to retry shortly.",
		"- If status is failed, apologize and ask the user to retry or rephrase.",
		"- If status is completed_no_output, apologize and ask for a clearer request.",
		"- If artifact_path is present, mention that an artifact was saved and include the path.",
		"",
		"Context:",
		"- channel: " + strings.TrimSpace(channel),
		"- status: " + status,
		"- assistant_output: " + quoteBridgeField(output, 700),
		"- error_summary: " + quoteBridgeField(errText, 500),
		"- tool_activity_summary: " + quoteBridgeField(toolSummary, 700),
		"- artifact_path: " + quoteBridgeField(artifactPath, 240),
		"",
		"Return only the final user-facing message.",
	}, "\n")
	reqCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	result, err := model.Generate(reqCtx, agent.ModelRequest{
		SystemPrompt: "You write concise, user-facing bridge updates.",
		Message:      prompt,
	})
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(result.FinalText)
	if text == "" {
		return "", errors.New("empty bridge outcome reply")
	}
	return text, nil
}

func quoteBridgeField(value string, maxChars int) string {
	clean := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if clean == "" {
		return "(none)"
	}
	if maxChars > 0 && len(clean) > maxChars {
		clean = strings.TrimSpace(clean[:maxChars]) + "..."
	}
	return clean
}

func queueChannelMessage(ctx context.Context, connector *chat.Connector, defaultAgentID, source, userID, roomID, requestedAgentID, text string, contentParts []messagecontent.Part, thinkingMode string) (chat.Result, error) {
	if connector == nil {
		return chat.Result{}, errors.New("chat connector is disabled")
	}
	agentID := strings.TrimSpace(requestedAgentID)
	if agentID == "" {
		agentID = strings.TrimSpace(defaultAgentID)
	}
	if agentID == "" {
		agentID = "default"
	}
	return connector.HandleMessage(ctx, chat.Message{UserID: userID, RoomID: roomID, AgentID: agentID, Source: source, Text: text, ContentParts: append([]messagecontent.Part(nil), contentParts...), ThinkingMode: thinkingMode})
}

type scopedChatAdapter struct {
	connector      *chat.Connector
	source         string
	defaultAgentID string
	allow          *chat.Allowlist
	limiter        *chat.RateLimiter
}

func (a scopedChatAdapter) HandleMessage(ctx context.Context, msg httpchannel.ChatMessage) (httpchannel.ChatResponse, error) {
	if a.allow != nil && !a.allow.MessageAllowed(msg.UserID, msg.RoomID) {
		return httpchannel.ChatResponse{}, chat.ErrNotAllowlisted
	}
	if a.limiter != nil {
		if allowed, retryAfter := a.limiter.AllowWithDetails(msg.UserID + ":" + msg.RoomID); !allowed {
			return httpchannel.ChatResponse{}, chat.NewRateLimitError("sender", retryAfter)
		}
	}
	agentID := strings.TrimSpace(msg.AgentID)
	if agentID == "" {
		agentID = strings.TrimSpace(a.defaultAgentID)
	}
	queued, err := a.connector.HandleMessage(ctx, chat.Message{UserID: msg.UserID, RoomID: msg.RoomID, AgentID: agentID, Source: a.source, Text: msg.Message, ContentParts: append([]messagecontent.Part(nil), msg.ContentParts...), ThinkingMode: msg.ThinkingMode})
	if err != nil {
		return httpchannel.ChatResponse{}, err
	}
	return httpchannel.ChatResponse{ID: queued.ID, Status: queued.Status, Response: queued.Response, SessionID: queued.SessionID}, nil
}

func providerForDoctor(cfg config.Config) (config.ProviderEndpointConfig, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Model.Provider)) {
	case "openai":
		return cfg.Providers.OpenAI, nil
	case "openrouter":
		return cfg.Providers.OpenRouter, nil
	case "requesty":
		return cfg.Providers.Requesty, nil
	case "hatz":
		return cfg.Providers.Hatz, nil
	case "zai":
		return cfg.Providers.ZAI, nil
	case "openai_compat":
		return cfg.Providers.OpenAICompat, nil
	default:
		return config.ProviderEndpointConfig{}, errors.New("unsupported provider")
	}
}

func handleSetup(args []string) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	force := fs.Bool("force", false, "overwrite existing config")
	_ = fs.Parse(args)

	eng, err := runtime.NewEngine(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := eng.Init("default", *force); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	cfgPath := filepath.Join(".openclawssy", "config.json")
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	in := bufio.NewReader(os.Stdin)
	fmt.Println("Openclawssy guided setup (ZAI Coding Plan Edition)")
	fmt.Println("Default: ZAI provider with GLM-4.7 model")
	fmt.Println("Get your API key at: https://z.ai/subscribe")
	fmt.Println("Press Enter to accept defaults.")

	cfg.Model.Provider = prompt(in, "Provider (zai=GLM-4.7 Coding Plan)", cfg.Model.Provider)
	cfg.Model.Name = prompt(in, "Model name", cfg.Model.Name)

	apiKey := prompt(in, "Provider API key (stored encrypted; optional if env used)", "")

	tls := prompt(in, "Enable HTTPS dashboard? [y/N]", "N")
	if strings.EqualFold(tls, "y") {
		cfg.Server.TLSEnabled = true
		if err := ensureSelfSigned(cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile); err != nil {
			fmt.Fprintln(os.Stderr, "warning: failed to create certs:", err)
		}
	}

	discordEnabled := prompt(in, "Enable Discord bot bridge? [y/N]", "N")
	if strings.EqualFold(discordEnabled, "y") {
		cfg.Discord.Enabled = true
		discordToken := prompt(in, "Discord bot token (stored encrypted; optional if env used)", "")
		if discordToken != "" {
			cfg.Discord.Token = ""
			if err := ensureMasterKey(cfg.Secrets.MasterKeyFile); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			store, err := secrets.NewStore(cfg)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			if err := store.Set("discord/bot_token", discordToken); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
		}
	}

	telegramEnabled := prompt(in, "Enable Telegram bot bridge? [y/N]", "N")
	if strings.EqualFold(telegramEnabled, "y") {
		cfg.Telegram.Enabled = true
		telegramToken := prompt(in, "Telegram bot token (stored encrypted; optional if env used)", "")
		if telegramToken != "" {
			cfg.Telegram.Token = ""
			if err := ensureMasterKey(cfg.Secrets.MasterKeyFile); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			store, err := secrets.NewStore(cfg)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			if err := store.Set("telegram/bot_token", telegramToken); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
		}
	}

	sandboxActive := true
	if value, ok, err := boolEnv(envSandboxActive); err != nil {
		fmt.Fprintf(os.Stderr, "warning: ignoring invalid %s: %v\n", envSandboxActive, err)
	} else if ok {
		sandboxActive = value
	} else {
		sandboxEnabled := prompt(in, "Enable Docker sandbox for isolated agent runs? (recommended) [Y/n]", "Y")
		sandboxActive = !strings.EqualFold(sandboxEnabled, "n")
	}

	if sandboxActive {
		cfg.Sandbox.Active = true
		cfg.Sandbox.Provider = "docker"
		cfg.Shell.EnableExec = true
		fmt.Println("Docker sandbox enabled. Agent workspace runs in a separate isolated container.")
		fmt.Println("The backend talks to Docker via the Unix socket at /var/run/docker.sock.")
		fmt.Println("Your user must be in the 'docker' group, or use sudo.")

		hardenedEnabled := false
		if value, ok, err := boolEnv(envSandboxDockerHardened); err != nil {
			fmt.Fprintf(os.Stderr, "warning: ignoring invalid %s: %v\n", envSandboxDockerHardened, err)
		} else if ok {
			hardenedEnabled = value
		} else {
			hardened := prompt(in, "Enable hardened Docker sandbox mode? [y/N]", "N")
			hardenedEnabled = strings.EqualFold(hardened, "y")
		}

		if hardenedEnabled {
			cfg.Sandbox.Docker.Hardened = true
			cfg.Sandbox.Docker.PidsLimit = 256
			cfg.Sandbox.Docker.AllowedImages = []string{cfg.Sandbox.Docker.Image}
			fmt.Println("Hardened mode enabled: drops all caps, enables no-new-privileges, read-only rootfs, tmpfs mounts, and PID limit.")

			requireDedicated := true
			if value, ok, err := boolEnv(envSandboxRequireDedicatedDaemon); err != nil {
				fmt.Fprintf(os.Stderr, "warning: ignoring invalid %s: %v\n", envSandboxRequireDedicatedDaemon, err)
			} else if ok {
				requireDedicated = value
			} else {
				dedicated := prompt(in, "Require a dedicated Docker daemon endpoint? (recommended for hardened mode) [Y/n]", "Y")
				requireDedicated = !strings.EqualFold(dedicated, "n")
			}

			if requireDedicated {
				cfg.Sandbox.Docker.RequireDedicatedDaemon = true
				if host, ok := stringEnv(envSandboxDockerHost); ok {
					cfg.Sandbox.Docker.Host = host
				} else {
					cfg.Sandbox.Docker.Host = prompt(in, "Dedicated Docker host (unix://, tcp://, or ssh://)", "unix:///var/run/openclawssy-docker.sock")
				}
			}
		}
	}

	if apiKey != "" {
		if err := ensureMasterKey(cfg.Secrets.MasterKeyFile); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		store, err := secrets.NewStore(cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := store.Set("provider/"+strings.ToLower(cfg.Model.Provider)+"/api_key", apiKey); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}

	if err := config.Save(cfgPath, cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	fmt.Println("Setup complete.")
	fmt.Println("Next:")
	fmt.Println("1) openclawssy doctor -v")
	if cfg.Sandbox.Active && cfg.Sandbox.Provider == "docker" {
		fmt.Println("2) openclawssy serve --token <token> --sandbox-active --sandbox-provider docker")
		fmt.Println("   Or use Docker: docker-compose up")
	} else {
		fmt.Println("2) openclawssy serve --token <token>")
	}
	if cfg.Server.TLSEnabled {
		fmt.Printf("3) open https://%s:%d/dashboard\n", cfg.Server.BindAddress, cfg.Server.Port)
	}
	return 0
}

func prompt(r *bufio.Reader, label, def string) string {
	fmt.Printf("%s [%s]: ", label, def)
	v, _ := r.ReadString('\n')
	v = strings.TrimSpace(v)
	if v == "" {
		return def
	}
	return v
}

func boolEnv(key string) (bool, bool, error) {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return false, false, nil
	}
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "1", "t", "true", "y", "yes", "on":
		return true, true, nil
	case "0", "f", "false", "n", "no", "off":
		return false, true, nil
	default:
		return false, true, fmt.Errorf("%q (expected true/false)", raw)
	}
}

func stringEnv(key string) (string, bool) {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return "", false
	}
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", false
	}
	return v, true
}

func ensureMasterKey(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if _, err := secrets.GenerateAndWriteMasterKey(path); err != nil {
		return err
	}
	return nil
}

func ensureSelfSigned(certPath, keyPath string) error {
	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			return nil
		}
	}
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "openclawssy.local"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		DNSNames:     []string{"localhost"},
		IPAddresses:  nil,
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		return err
	}
	certOut, err := os.Create(certPath)
	if err != nil {
		return err
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		return err
	}
	if err := os.Chmod(certPath, 0o600); err != nil {
		return err
	}
	keyOut, err := os.Create(keyPath)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}); err != nil {
		return err
	}
	return os.Chmod(keyPath, 0o600)
}
