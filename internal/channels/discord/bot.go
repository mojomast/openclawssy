package discord

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"openclawssy/internal/channels/chat"
	"openclawssy/internal/config"
)

const (
	defaultPollInterval   = 1200 * time.Millisecond
	defaultPollTimeout    = 2 * time.Minute
	defaultDiscordMaxSize = 1900
)

type Message struct {
	UserID       string
	RoomID       string
	AgentID      string
	Source       string
	Text         string
	ThinkingMode string
}

type Response struct {
	ID       string
	Status   string
	Response string
}

type RunStatus = chat.RunStatus

type MessageHandler func(ctx context.Context, msg Message) (Response, error)
type RunStatusFunc = chat.RunStatusFunc

type Bot struct {
	cfg       config.DiscordConfig
	allow     *chat.Allowlist
	limiter   *chat.RateLimiter
	handler   MessageHandler
	runStatus RunStatusFunc
	session   *discordgo.Session
	closeOnce sync.Once
}

func New(cfg config.Config, handler MessageHandler, runStatus RunStatusFunc) (*Bot, error) {
	token := strings.TrimSpace(cfg.Discord.Token)
	if token == "" && cfg.Discord.TokenEnv != "" {
		token = strings.TrimSpace(os.Getenv(cfg.Discord.TokenEnv))
	}
	if token == "" {
		return nil, errors.New("discord token is required")
	}
	allow := chat.NewAllowlist(cfg.Discord.AllowUsers, cfg.Discord.AllowChannels)
	limiter := chat.NewRateLimiter(cfg.Discord.RateLimitPerMin, time.Minute)
	s, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}
	b := &Bot{cfg: cfg.Discord, allow: allow, limiter: limiter, handler: handler, runStatus: runStatus, session: s}
	s.AddHandler(b.onMessage)
	s.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent
	return b, nil
}

func (b *Bot) Start() error {
	return b.session.Open()
}

func (b *Bot) Stop() error {
	var err error
	b.closeOnce.Do(func() { err = b.session.Close() })
	return err
}

func (b *Bot) onMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot {
		return
	}
	content := normalizeInboundMessage(strings.TrimSpace(m.Content), b.cfg.CommandPrefix)
	if content == "" {
		return
	}
	content, thinkingMode, parseErr := parseThinkingOverride(content)
	if parseErr != nil {
		_, _ = s.ChannelMessageSendReply(m.ChannelID, formatDiscordError(parseErr), m.Reference())
		return
	}

	if len(b.cfg.AllowGuilds) > 0 && !contains(b.cfg.AllowGuilds, m.GuildID) {
		return
	}
	if b.allow != nil && !b.allow.MessageAllowed(m.Author.ID, m.ChannelID) {
		return
	}
	if b.limiter != nil {
		if allowed, retryAfter := b.limiter.AllowWithDetails(m.Author.ID + ":" + m.ChannelID); !allowed {
			_, _ = s.ChannelMessageSendReply(m.ChannelID, formatDiscordRateLimit(retryAfter), m.Reference())
			return
		}
	}
	if b.handler == nil {
		_, _ = s.ChannelMessageSendReply(m.ChannelID, "chat handler is not configured", m.Reference())
		return
	}

	agentID := b.cfg.DefaultAgentID
	if agentID == "" {
		agentID = "default"
	}
	res, err := b.handler(context.Background(), Message{
		UserID:       m.Author.ID,
		RoomID:       m.ChannelID,
		AgentID:      agentID,
		Source:       "discord",
		Text:         content,
		ThinkingMode: thinkingMode,
	})
	if err != nil {
		_, _ = s.ChannelMessageSendReply(m.ChannelID, formatDiscordError(err), m.Reference())
		return
	}

	if strings.TrimSpace(res.Response) != "" {
		b.sendChunked(s, m, res.Response)
	}

	if strings.TrimSpace(res.ID) == "" {
		return
	}

	if strings.TrimSpace(res.Response) == "" {
		_, _ = s.ChannelMessageSendReply(m.ChannelID, "queued run `"+res.ID+"`", m.Reference())
	}
	if b.runStatus == nil {
		return
	}

	go b.awaitAndPostResult(s, m, res.ID)
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func normalizeInboundMessage(content, commandPrefix string) string {
	return chat.NormalizeInboundMessage(content, commandPrefix)
}

func parseThinkingOverride(content string) (string, string, error) {
	return chat.ParseThinkingOverride(content)
}

func formatDiscordError(err error) string {
	return chat.FormatBridgeError(err, "channel or user scope")
}

func formatDiscordRateLimit(retryAfter time.Duration) string {
	return chat.FormatRateLimit(retryAfter)
}

func (b *Bot) awaitAndPostResult(s *discordgo.Session, m *discordgo.MessageCreate, runID string) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultPollTimeout)
	defer cancel()

	run, err := waitForTerminalRun(ctx, runID, b.runStatus, defaultPollInterval)
	if err != nil {
		_, _ = s.ChannelMessageSendReply(m.ChannelID, "failed to fetch run `"+runID+"`: "+err.Error(), m.Reference())
		return
	}

	if strings.EqualFold(strings.TrimSpace(run.Status), "failed") {
		msg := "run `" + runID + "` failed"
		if strings.TrimSpace(run.Error) != "" {
			msg += ": " + run.Error
		}
		if toolSummary := formatToolActivity(runID, run.Trace); toolSummary != "" {
			b.sendChunked(s, m, toolSummary)
		}
		_, _ = s.ChannelMessageSendReply(m.ChannelID, msg, m.Reference())
		return
	}

	if toolSummary := formatToolActivity(runID, run.Trace); toolSummary != "" {
		b.sendChunked(s, m, toolSummary)
	}

	final := strings.TrimSpace(run.Output)
	if final == "" {
		final = "run completed without assistant output; check run trace/tool activity for details"
	}
	if strings.TrimSpace(run.ArtifactPath) != "" {
		final = fmt.Sprintf("%s\n\nartifact: `%s`", final, run.ArtifactPath)
	}
	b.sendChunked(s, m, final)
}

func formatToolActivity(runID string, trace map[string]any) string {
	return chat.FormatToolActivity(runID, trace)
}

func waitForTerminalRun(ctx context.Context, runID string, runStatus RunStatusFunc, interval time.Duration) (RunStatus, error) {
	return chat.WaitForTerminalRun(ctx, runID, runStatus, interval, defaultPollInterval)
}

func splitDiscordMessage(text string, maxLen int) []string {
	if maxLen <= 0 {
		maxLen = defaultDiscordMaxSize
	}
	return chat.SplitMessage(text, maxLen)
}

func (b *Bot) sendChunked(s *discordgo.Session, m *discordgo.MessageCreate, text string) {
	parts := splitDiscordMessage(text, defaultDiscordMaxSize)
	for i, part := range parts {
		if i == 0 {
			_, _ = s.ChannelMessageSendReply(m.ChannelID, part, m.Reference())
			continue
		}
		_, _ = s.ChannelMessageSend(m.ChannelID, part)
	}
}
