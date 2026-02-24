package telegram

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"openclawssy/internal/channels/chat"
	"openclawssy/internal/config"
)

const (
	defaultPollInterval    = 1200 * time.Millisecond
	defaultPollTimeout     = 2 * time.Minute
	defaultTelegramMaxSize = 4096
	// defaultUpdateTimeout is Telegram long-poll timeout in seconds.
	defaultUpdateTimeout = 30
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
	cfg       config.TelegramConfig
	allow     *chat.Allowlist
	limiter   *chat.RateLimiter
	handler   MessageHandler
	runStatus RunStatusFunc
	api       *tgbotapi.BotAPI
	closeOnce sync.Once
	wg        sync.WaitGroup
}

func New(cfg config.Config, handler MessageHandler, runStatus RunStatusFunc) (*Bot, error) {
	token := strings.TrimSpace(cfg.Telegram.Token)
	if token == "" && cfg.Telegram.TokenEnv != "" {
		token = strings.TrimSpace(os.Getenv(cfg.Telegram.TokenEnv))
	}
	if token == "" {
		return nil, errors.New("telegram token is required")
	}
	allow := chat.NewAllowlist(cfg.Telegram.AllowUsers, cfg.Telegram.AllowChats)
	limiter := chat.NewRateLimiter(cfg.Telegram.RateLimitPerMin, time.Minute)
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}
	return &Bot{cfg: cfg.Telegram, allow: allow, limiter: limiter, handler: handler, runStatus: runStatus, api: api}, nil
}

func (b *Bot) Start() error {
	if b == nil || b.api == nil {
		return errors.New("telegram bot is not configured")
	}
	updateCfg := tgbotapi.NewUpdate(0)
	updateCfg.Timeout = defaultUpdateTimeout
	updates := b.api.GetUpdatesChan(updateCfg)
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for update := range updates {
			b.onUpdate(update)
		}
	}()
	return nil
}

func (b *Bot) Stop() error {
	if b == nil || b.api == nil {
		return nil
	}
	b.closeOnce.Do(func() {
		b.api.StopReceivingUpdates()
		b.wg.Wait()
	})
	return nil
}

func (b *Bot) onUpdate(update tgbotapi.Update) {
	m := update.Message
	if m == nil || m.From == nil || m.From.IsBot {
		return
	}
	content := normalizeInboundMessage(strings.TrimSpace(m.Text), b.cfg.CommandPrefix)
	if content == "" {
		return
	}
	content, thinkingMode, parseErr := parseThinkingOverride(content)
	if parseErr != nil {
		b.sendMessage(m.Chat.ID, m.MessageID, formatTelegramError(parseErr))
		return
	}

	userID := strconv.FormatInt(m.From.ID, 10)
	chatID := strconv.FormatInt(m.Chat.ID, 10)
	if b.allow != nil && !b.allow.MessageAllowed(userID, chatID) {
		return
	}
	if b.limiter != nil {
		if allowed, retryAfter := b.limiter.AllowWithDetails(userID + ":" + chatID); !allowed {
			b.sendMessage(m.Chat.ID, m.MessageID, formatTelegramRateLimit(retryAfter))
			return
		}
	}
	if b.handler == nil {
		b.sendMessage(m.Chat.ID, m.MessageID, "chat handler is not configured")
		return
	}

	agentID := b.cfg.DefaultAgentID
	if agentID == "" {
		agentID = "default"
	}
	res, err := b.handler(context.Background(), Message{
		UserID:       userID,
		RoomID:       chatID,
		AgentID:      agentID,
		Source:       "telegram",
		Text:         content,
		ThinkingMode: thinkingMode,
	})
	if err != nil {
		b.sendMessage(m.Chat.ID, m.MessageID, formatTelegramError(err))
		return
	}

	if strings.TrimSpace(res.Response) != "" {
		b.sendChunked(m.Chat.ID, m.MessageID, res.Response)
	}
	if strings.TrimSpace(res.ID) == "" {
		return
	}
	if strings.TrimSpace(res.Response) == "" {
		b.sendMessage(m.Chat.ID, m.MessageID, "queued run `"+res.ID+"`")
	}
	if b.runStatus == nil {
		return
	}

	go b.awaitAndPostResult(m.Chat.ID, m.MessageID, res.ID)
}

func normalizeInboundMessage(content, commandPrefix string) string {
	return chat.NormalizeInboundMessage(content, commandPrefix)
}

func parseThinkingOverride(content string) (string, string, error) {
	return chat.ParseThinkingOverride(content)
}

func formatTelegramError(err error) string {
	return chat.FormatBridgeError(err, "chat or user scope")
}

func formatTelegramRateLimit(retryAfter time.Duration) string {
	return chat.FormatRateLimit(retryAfter)
}

func (b *Bot) awaitAndPostResult(chatID int64, replyTo int, runID string) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultPollTimeout)
	defer cancel()

	run, err := waitForTerminalRun(ctx, runID, b.runStatus, defaultPollInterval)
	if err != nil {
		b.sendMessage(chatID, replyTo, "failed to fetch run `"+runID+"`: "+err.Error())
		return
	}

	if strings.EqualFold(strings.TrimSpace(run.Status), "failed") {
		msg := "run `" + runID + "` failed"
		if strings.TrimSpace(run.Error) != "" {
			msg += ": " + run.Error
		}
		if toolSummary := formatToolActivity(runID, run.Trace); toolSummary != "" {
			b.sendChunked(chatID, replyTo, toolSummary)
		}
		b.sendMessage(chatID, replyTo, msg)
		return
	}

	if toolSummary := formatToolActivity(runID, run.Trace); toolSummary != "" {
		b.sendChunked(chatID, replyTo, toolSummary)
	}

	final := strings.TrimSpace(run.Output)
	if final == "" {
		final = "run completed without assistant output; check run trace/tool activity for details"
	}
	if strings.TrimSpace(run.ArtifactPath) != "" {
		final = fmt.Sprintf("%s\n\nartifact: `%s`", final, run.ArtifactPath)
	}
	b.sendChunked(chatID, replyTo, final)
}

func formatToolActivity(runID string, trace map[string]any) string {
	return chat.FormatToolActivity(runID, trace)
}

func waitForTerminalRun(ctx context.Context, runID string, runStatus RunStatusFunc, interval time.Duration) (RunStatus, error) {
	return chat.WaitForTerminalRun(ctx, runID, runStatus, interval, defaultPollInterval)
}

func splitTelegramMessage(text string, maxLen int) []string {
	if maxLen <= 0 {
		maxLen = defaultTelegramMaxSize
	}
	return chat.SplitMessage(text, maxLen)
}

func (b *Bot) sendChunked(chatID int64, replyTo int, text string) {
	parts := splitTelegramMessage(text, defaultTelegramMaxSize)
	for i, part := range parts {
		if i == 0 {
			b.sendMessage(chatID, replyTo, part)
			continue
		}
		b.sendMessage(chatID, 0, part)
	}
}

func (b *Bot) sendMessage(chatID int64, replyTo int, text string) {
	if b == nil || b.api == nil {
		return
	}
	msg := tgbotapi.NewMessage(chatID, text)
	if replyTo > 0 {
		msg.ReplyToMessageID = replyTo
	}
	_, _ = b.api.Send(msg)
}
