package telegram

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	_ "golang.org/x/image/webp"
	"openclawssy/internal/channels/chat"
	"openclawssy/internal/config"
	"openclawssy/internal/messagecontent"
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
	ContentParts []messagecontent.Part
	ThinkingMode string
}

type Response struct {
	ID       string
	Status   string
	Response string
}

type RunStatus = chat.RunStatus

type OutcomeInput struct {
	RunID        string
	Status       string
	Output       string
	Error        string
	ArtifactPath string
	ToolSummary  string
}

type OutcomeResponder func(ctx context.Context, input OutcomeInput) (string, error)

type MessageHandler func(ctx context.Context, msg Message) (Response, error)
type RunStatusFunc = chat.RunStatusFunc

type Bot struct {
	cfg        config.TelegramConfig
	allow      *chat.Allowlist
	limiter    *chat.RateLimiter
	handler    MessageHandler
	runStatus  RunStatusFunc
	outcome    OutcomeResponder
	api        *tgbotapi.BotAPI
	httpClient *http.Client
	closeOnce  sync.Once
	wg         sync.WaitGroup
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
	return &Bot{cfg: cfg.Telegram, allow: allow, limiter: limiter, handler: handler, runStatus: runStatus, api: api, httpClient: &http.Client{Timeout: 20 * time.Second}}, nil
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

func (b *Bot) SetOutcomeResponder(responder OutcomeResponder) {
	if b == nil {
		return
	}
	b.outcome = responder
}

func (b *Bot) onUpdate(update tgbotapi.Update) {
	m := update.Message
	if m == nil || m.From == nil || m.From.IsBot {
		slog.Debug("telegram: skipping update", "has_message", m != nil)
		return
	}
	rawText := strings.TrimSpace(m.Text)
	slog.Debug("telegram: received message", "from", m.From.UserName, "from_id", m.From.ID, "chat_id", m.Chat.ID, "text", rawText, "command_prefix", b.cfg.CommandPrefix)
	content, contentParts, mediaErr := b.normalizeIncomingContent(m)
	if mediaErr != nil {
		b.sendMessage(m.Chat.ID, m.MessageID, formatTelegramError(mediaErr))
		return
	}
	if content == "" && len(contentParts) == 0 {
		slog.Debug("telegram: message filtered out by normalizeInboundMessage", "raw", rawText, "prefix", b.cfg.CommandPrefix)
		return
	}
	thinkingMode := ""
	if strings.TrimSpace(content) != "" {
		parsedContent, parsedThinkingMode, parseErr := parseThinkingOverride(content)
		if parseErr != nil {
			slog.Debug("telegram: thinking parse error", "err", parseErr)
			b.sendMessage(m.Chat.ID, m.MessageID, formatTelegramError(parseErr))
			return
		}
		content = parsedContent
		thinkingMode = parsedThinkingMode
	}

	userID := strconv.FormatInt(m.From.ID, 10)
	chatID := strconv.FormatInt(m.Chat.ID, 10)
	if b.allow != nil && !b.allow.MessageAllowed(userID, chatID) &&
		!b.allow.MessageAllowed(m.From.UserName, chatID) {
		slog.Debug("telegram: message blocked by allowlist", "user_id", userID, "username", m.From.UserName, "chat_id", chatID)
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
		ContentParts: contentParts,
		ThinkingMode: thinkingMode,
	})
	if err != nil {
		b.sendMessage(m.Chat.ID, m.MessageID, formatTelegramError(err))
		return
	}

	if strings.TrimSpace(res.ID) == "" {
		// Synchronous response with no run ID — send it directly.
		if strings.TrimSpace(res.Response) != "" {
			b.sendChunked(m.Chat.ID, m.MessageID, res.Response)
		}
		return
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

func (b *Bot) normalizeIncomingContent(m *tgbotapi.Message) (string, []messagecontent.Part, error) {
	content := normalizeInboundMessage(strings.TrimSpace(m.Text), b.cfg.CommandPrefix)
	if content == "" && m.Sticker == nil {
		return "", nil, nil
	}
	parts := make([]messagecontent.Part, 0, 2)
	if strings.TrimSpace(content) != "" {
		parts = append(parts, messagecontent.Part{Type: messagecontent.TypeText, Text: strings.TrimSpace(content)})
	}
	if m.Sticker != nil {
		imagePart, err := b.stickerPart(m.Sticker)
		if err != nil {
			return "", nil, err
		}
		parts = append(parts, imagePart)
	}
	return content, messagecontent.Normalize(parts), nil
}

func (b *Bot) stickerPart(sticker *tgbotapi.Sticker) (messagecontent.Part, error) {
	if sticker == nil || strings.TrimSpace(sticker.FileID) == "" {
		return messagecontent.Part{}, errors.New("telegram sticker is missing file id")
	}
	if sticker.IsAnimated {
		return messagecontent.Part{}, errors.New("animated telegram stickers are not supported yet")
	}
	url, err := b.api.GetFileDirectURL(sticker.FileID)
	if err != nil {
		return messagecontent.Part{}, fmt.Errorf("download telegram sticker url: %w", err)
	}
	resp, err := b.httpClient.Get(url)
	if err != nil {
		return messagecontent.Part{}, fmt.Errorf("download telegram sticker: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return messagecontent.Part{}, fmt.Errorf("download telegram sticker: status=%d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return messagecontent.Part{}, fmt.Errorf("read telegram sticker: %w", err)
	}
	if len(body) == 0 {
		return messagecontent.Part{}, errors.New("telegram sticker download returned empty body")
	}
	pngBody, err := convertStickerToPNG(body)
	if err != nil {
		return messagecontent.Part{}, fmt.Errorf("convert telegram sticker to png: %w", err)
	}
	return messagecontent.Part{Type: messagecontent.TypeImage, MIMEType: "image/png", Data: base64.StdEncoding.EncodeToString(pngBody)}, nil
}

func convertStickerToPNG(body []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	return encodePNG(img)
}

func encodePNG(img image.Image) ([]byte, error) {
	if img == nil {
		return nil, errors.New("nil image")
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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

	// Send typing indicator every 4s while waiting for the run to complete.
	// Telegram's typing indicator expires after ~5s so we refresh slightly before that.
	stopTyping := make(chan struct{})
	go func() {
		b.sendTyping(chatID)
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				b.sendTyping(chatID)
			case <-stopTyping:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	run, err := waitForTerminalRun(ctx, runID, b.runStatus, defaultPollInterval)
	close(stopTyping)
	if err != nil {
		msg := b.renderOutcomeText(context.Background(), runID, RunStatus{Status: "status_lookup_error", Error: err.Error()}, "I hit a temporary issue while checking your result. Please try again.")
		b.sendMessage(chatID, replyTo, msg)
		return
	}
	if strings.EqualFold(strings.TrimSpace(run.Status), "timeout") {
		msg := b.renderOutcomeText(context.Background(), runID, run, "Thanks for your patience - I am still working on your request.")
		b.sendChunked(chatID, replyTo, msg)
		return
	}

	if strings.EqualFold(strings.TrimSpace(run.Status), "failed") {
		msg := b.renderOutcomeText(ctx, runID, run, "I ran into an issue while working on that request. Please try again.")
		b.sendMessage(chatID, replyTo, msg)
		return
	}

	final := strings.TrimSpace(run.Output)
	if final == "" {
		renderRun := run
		renderRun.Status = "completed_no_output"
		final = b.renderOutcomeText(ctx, runID, renderRun, "I could not produce a useful response this time. Please try again.")
	}
	b.sendChunked(chatID, replyTo, final)
	return
}

func formatToolActivity(runID string, trace map[string]any) string {
	return chat.FormatToolActivity(runID, trace)
}

func waitForTerminalRun(ctx context.Context, runID string, runStatus RunStatusFunc, interval time.Duration) (RunStatus, error) {
	return chat.WaitForTerminalRun(ctx, runID, runStatus, interval, defaultPollInterval)
}

func (b *Bot) renderOutcomeText(ctx context.Context, runID string, run RunStatus, fallback string) string {
	fallback = strings.TrimSpace(fallback)
	if b == nil || b.outcome == nil {
		return fallback
	}
	if ctx == nil {
		ctx = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	out, err := b.outcome(reqCtx, OutcomeInput{
		RunID:        strings.TrimSpace(runID),
		Status:       strings.TrimSpace(run.Status),
		Output:       strings.TrimSpace(run.Output),
		Error:        strings.TrimSpace(run.Error),
		ArtifactPath: "",
		ToolSummary:  strings.TrimSpace(formatToolActivity(runID, run.Trace)),
	})
	if err != nil {
		return fallback
	}
	if strings.TrimSpace(out) == "" {
		return fallback
	}
	return strings.TrimSpace(out)
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

func (b *Bot) sendTyping(chatID int64) {
	if b == nil || b.api == nil {
		return
	}
	_, _ = b.api.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping))
}
