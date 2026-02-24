package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"openclawssy/internal/config"
	"strings"
)

type InitInput struct {
	Workspace string
	AgentID   string
	Force     bool
}

type AskInput struct {
	AgentID      string
	Message      string
	ThinkingMode string
}

type RunInput struct {
	AgentID     string
	Message     string
	MessageFile string
	Detached    bool
}

type DoctorInput struct {
	Verbose bool
}

type CronInput struct {
	Command string
	Args    []string
}

type ServeInput struct {
	Addr            string
	Token           string
	RunsFile        string
	JobsFile        string
	SandboxProvider string
	SandboxActive   bool
}

type InitService interface {
	Init(ctx context.Context, input InitInput) error
}

type AskService interface {
	Ask(ctx context.Context, input AskInput) (string, error)
}

type RunService interface {
	Run(ctx context.Context, input RunInput) (string, error)
}

type DoctorService interface {
	Doctor(ctx context.Context, input DoctorInput) (string, error)
}

type CronService interface {
	Cron(ctx context.Context, input CronInput) (string, error)
}

type Handlers struct {
	Init   InitService
	Ask    AskService
	Run    RunService
	Doctor DoctorService
	Cron   CronService

	Out io.Writer
	Err io.Writer
}

func (h Handlers) HandleInit(ctx context.Context, args []string) int {
	if h.Init == nil {
		return h.fail(errors.New("init service is not configured"))
	}

	var input InitInput
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(h.errorWriter())
	fs.StringVar(&input.Workspace, "workspace", ".", "workspace root")
	fs.StringVar(&input.AgentID, "agent", "default", "agent id")
	fs.BoolVar(&input.Force, "force", false, "overwrite existing files")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if err := h.Init.Init(ctx, input); err != nil {
		return h.fail(err)
	}

	return 0
}

func (h Handlers) HandleAsk(ctx context.Context, args []string) int {
	if h.Ask == nil {
		return h.fail(errors.New("ask service is not configured"))
	}

	var input AskInput
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	fs.SetOutput(h.errorWriter())
	fs.StringVar(&input.AgentID, "agent", "default", "agent id")
	fs.StringVar(&input.Message, "message", "", "message to send")
	fs.StringVar(&input.ThinkingMode, "thinking", "", "thinking mode override (never|on_error|always)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if input.Message == "" {
		return h.fail(errors.New("-message is required"))
	}
	if strings.TrimSpace(input.ThinkingMode) != "" && !config.IsValidThinkingMode(input.ThinkingMode) {
		return h.fail(errors.New("-thinking must be one of never|on_error|always"))
	}
	input.ThinkingMode = config.NormalizeThinkingMode(input.ThinkingMode)

	output, err := h.Ask.Ask(ctx, input)
	if err != nil {
		return h.fail(err)
	}
	_, _ = fmt.Fprintln(h.outWriter(), output)
	return 0
}

func (h Handlers) HandleRun(ctx context.Context, args []string) int {
	if h.Run == nil {
		return h.fail(errors.New("run service is not configured"))
	}

	var input RunInput
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(h.errorWriter())
	fs.StringVar(&input.AgentID, "agent", "default", "agent id")
	fs.StringVar(&input.Message, "message", "", "run message")
	fs.StringVar(&input.MessageFile, "message-file", "", "path to message file")
	fs.BoolVar(&input.Detached, "detach", false, "queue and return immediately")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if input.Message == "" && input.MessageFile == "" {
		return h.fail(errors.New("either -message or -message-file is required"))
	}

	output, err := h.Run.Run(ctx, input)
	if err != nil {
		return h.fail(err)
	}
	_, _ = fmt.Fprintln(h.outWriter(), output)
	return 0
}

func (h Handlers) HandleDoctor(ctx context.Context, args []string) int {
	if h.Doctor == nil {
		return h.fail(errors.New("doctor service is not configured"))
	}

	var input DoctorInput
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(h.errorWriter())
	fs.BoolVar(&input.Verbose, "v", false, "verbose output")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	output, err := h.Doctor.Doctor(ctx, input)
	if err != nil {
		return h.fail(err)
	}
	_, _ = fmt.Fprintln(h.outWriter(), output)
	return 0
}

func (h Handlers) HandleCron(ctx context.Context, args []string) int {
	if h.Cron == nil {
		return h.fail(errors.New("cron service is not configured"))
	}

	var input CronInput
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		input.Command = args[0]
		input.Args = args[1:]
		output, err := h.Cron.Cron(ctx, input)
		if err != nil {
			return h.fail(err)
		}
		_, _ = fmt.Fprintln(h.outWriter(), output)
		return 0
	}

	fs := flag.NewFlagSet("cron", flag.ContinueOnError)
	fs.SetOutput(h.errorWriter())
	fs.StringVar(&input.Command, "command", "list", "cron command (list/add/remove/delete/pause/resume)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	input.Args = fs.Args()

	output, err := h.Cron.Cron(ctx, input)
	if err != nil {
		return h.fail(err)
	}
	_, _ = fmt.Fprintln(h.outWriter(), output)
	return 0
}

func ParseServeArgs(args []string) (ServeInput, error) {
	input := ServeInput{}
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.StringVar(&input.Addr, "addr", "127.0.0.1:8080", "listen address")
	fs.StringVar(&input.Token, "token", "", "bearer token (required)")
	fs.StringVar(&input.RunsFile, "runs-file", ".openclawssy/runs.json", "run status store path")
	fs.StringVar(&input.JobsFile, "jobs-file", ".openclawssy/scheduler/jobs.json", "scheduler jobs store path")
	fs.StringVar(&input.SandboxProvider, "sandbox-provider", "", "sandbox provider: none, local, docker (overrides config)")
	fs.BoolVar(&input.SandboxActive, "sandbox-active", false, "enable sandbox (overrides config)")
	if err := fs.Parse(args); err != nil {
		return ServeInput{}, err
	}

	if input.Token == "" {
		return ServeInput{}, errors.New("-token is required")
	}

	return input, nil
}

func (h Handlers) outWriter() io.Writer {
	if h.Out != nil {
		return h.Out
	}
	return io.Discard
}

func (h Handlers) errorWriter() io.Writer {
	if h.Err != nil {
		return h.Err
	}
	return io.Discard
}

func (h Handlers) fail(err error) int {
	_, _ = fmt.Fprintln(h.errorWriter(), err)
	return 1
}
