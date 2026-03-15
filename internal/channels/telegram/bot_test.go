package telegram

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"openclawssy/internal/channels/chat"
	"openclawssy/internal/config"
	"openclawssy/internal/messagecontent"
)

func TestNormalizeInboundMessage(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		prefix string
		want   string
	}{
		{name: "plain content with no prefix", input: "hello", want: "hello"},
		{name: "command prefix message", input: "!ask hello", prefix: "!ask", want: "hello"},
		{name: "slash command bypasses prefix", input: "/new", prefix: "!ask", want: "/new"},
		{name: "non prefixed ignored", input: "hello", prefix: "!ask", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeInboundMessage(tc.input, tc.prefix)
			if got != tc.want {
				t.Fatalf("normalizeInboundMessage() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSplitTelegramMessage(t *testing.T) {
	parts := splitTelegramMessage("alpha\nbeta\ngamma", 7)
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}
	if parts[0] != "alpha" || parts[1] != "beta" || parts[2] != "gamma" {
		t.Fatalf("unexpected parts: %#v", parts)
	}

	long := strings.Repeat("x", 25)
	parts = splitTelegramMessage(long, 10)
	if len(parts) != 3 {
		t.Fatalf("expected 3 long chunks, got %d", len(parts))
	}
	for i, p := range parts {
		if len(p) > 10 {
			t.Fatalf("chunk %d too long: %d", i, len(p))
		}
	}
}

func TestWaitForTerminalRun(t *testing.T) {
	t.Run("completes after polling", func(t *testing.T) {
		calls := 0
		statusFn := func(ctx context.Context, runID string) (RunStatus, error) {
			_ = ctx
			_ = runID
			calls++
			if calls < 3 {
				return RunStatus{Status: "running"}, nil
			}
			return RunStatus{Status: "completed", Output: "done"}, nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		run, err := waitForTerminalRun(ctx, "run-1", statusFn, time.Millisecond)
		if err != nil {
			t.Fatalf("waitForTerminalRun() error = %v", err)
		}
		if run.Status != "completed" || run.Output != "done" {
			t.Fatalf("unexpected run result: %+v", run)
		}
	})

	t.Run("returns error from status func", func(t *testing.T) {
		wantErr := errors.New("boom")
		_, err := waitForTerminalRun(context.Background(), "run-1", func(ctx context.Context, runID string) (RunStatus, error) {
			_ = ctx
			_ = runID
			return RunStatus{}, wantErr
		}, time.Millisecond)
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected %v, got %v", wantErr, err)
		}
	})

	t.Run("returns timeout status when polling deadline expires", func(t *testing.T) {
		statusFn := func(ctx context.Context, runID string) (RunStatus, error) {
			_ = ctx
			_ = runID
			return RunStatus{Status: "running"}, nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
		defer cancel()
		run, err := waitForTerminalRun(ctx, "run-1", statusFn, time.Millisecond)
		if err != nil {
			t.Fatalf("waitForTerminalRun() error = %v", err)
		}
		if run.Status != "timeout" {
			t.Fatalf("expected timeout status, got %+v", run)
		}
	})

	t.Run("keeps upstream deadline errors when polling context is still active", func(t *testing.T) {
		_, err := waitForTerminalRun(context.Background(), "run-1", func(ctx context.Context, runID string) (RunStatus, error) {
			_ = ctx
			_ = runID
			return RunStatus{}, context.DeadlineExceeded
		}, time.Millisecond)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context deadline exceeded error, got %v", err)
		}
	})
}

func TestParseThinkingOverride(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantText  string
		wantMode  string
		wantError bool
	}{
		{name: "prefix text with override", in: "thinking=on_error summarize", wantText: "summarize", wantMode: "on_error"},
		{name: "plain text", in: "hello", wantText: "hello"},
		{name: "pass through slash command", in: "/resume chat_1", wantText: "/resume chat_1"},
		{name: "slash ask passes through", in: "/ask thinking=always summarize", wantText: "/ask thinking=always summarize"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			text, mode, err := parseThinkingOverride(tc.in)
			if tc.wantError {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if text != tc.wantText || mode != tc.wantMode {
				t.Fatalf("got text=%q mode=%q want text=%q mode=%q", text, mode, tc.wantText, tc.wantMode)
			}
		})
	}
}

func TestFormatTelegramErrorRateLimited(t *testing.T) {
	msg := formatTelegramError(chat.NewRateLimitError("sender", 2300*time.Millisecond))
	if msg != "rate limited, retry in 3s" {
		t.Fatalf("unexpected rate limit format: %q", msg)
	}

	msg = formatTelegramError(errors.New("sender is rate limited"))
	if msg != "rate limited, try again soon" {
		t.Fatalf("unexpected generic rate limit format: %q", msg)
	}

	msg = formatTelegramError(errors.New("chat sender is not allowlisted"))
	if msg != "not allowed in this chat or user scope" {
		t.Fatalf("unexpected allowlist format: %q", msg)
	}

	msg = formatTelegramError(errors.New("httpchannel: run queue is full"))
	if msg != "run queue is full, retry shortly" {
		t.Fatalf("unexpected queue-full format: %q", msg)
	}
}

func TestRenderOutcomeText(t *testing.T) {
	b := &Bot{}
	run := RunStatus{Status: "failed", Error: "boom", Trace: map[string]any{"tool_execution_results": []any{map[string]any{"tool": "fs.read", "summary": "read file"}}}}

	t.Run("falls back without responder", func(t *testing.T) {
		got := b.renderOutcomeText(context.Background(), "run-1", run, "fallback text")
		if got != "fallback text" {
			t.Fatalf("expected fallback text, got %q", got)
		}
	})

	t.Run("uses responder output", func(t *testing.T) {
		seen := OutcomeInput{}
		b.SetOutcomeResponder(func(ctx context.Context, input OutcomeInput) (string, error) {
			_ = ctx
			seen = input
			return "friendly reply", nil
		})
		got := b.renderOutcomeText(context.Background(), "run-1", run, "fallback text")
		if got != "friendly reply" {
			t.Fatalf("expected responder output, got %q", got)
		}
		if seen.Status != "failed" {
			t.Fatalf("expected status failed, got %q", seen.Status)
		}
		if !strings.Contains(seen.ToolSummary, "fs.read") {
			t.Fatalf("expected tool summary to include tool name, got %q", seen.ToolSummary)
		}
	})

	t.Run("falls back on responder error", func(t *testing.T) {
		b.SetOutcomeResponder(func(ctx context.Context, input OutcomeInput) (string, error) {
			_ = ctx
			_ = input
			return "", errors.New("unavailable")
		})
		got := b.renderOutcomeText(context.Background(), "run-1", run, "fallback text")
		if got != "fallback text" {
			t.Fatalf("expected fallback after responder error, got %q", got)
		}
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestNormalizeIncomingContentStickerAddsImagePrompt(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var sticker bytes.Buffer
	if err := png.Encode(&sticker, img); err != nil {
		t.Fatalf("encode sticker png: %v", err)
	}
	b := &Bot{
		cfg: config.TelegramConfig{},
		api: func() *tgbotapi.BotAPI {
			api, err := tgbotapi.NewBotAPIWithClient("tok", "https://api.telegram.org/bot%s/%s", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.String(), "/getMe") {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(`{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"bot","username":"bot"}}`)),
					}, nil
				}
				if strings.Contains(req.URL.String(), "/getFile") {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(`{"ok":true,"result":{"file_path":"sticker.webp"}}`)),
					}, nil
				}
				t.Fatalf("unexpected telegram api url: %s", req.URL.String())
				return nil, nil
			})})
			if err != nil {
				t.Fatalf("new bot api: %v", err)
			}
			return api
		}(),
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != "https://api.telegram.org/file/bottok/sticker.webp" {
				t.Fatalf("unexpected sticker url: %s", req.URL.String())
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"image/webp"}},
				Body:       io.NopCloser(bytes.NewReader(sticker.Bytes())),
			}, nil
		})},
	}
	text, parts, err := b.normalizeIncomingContent(&tgbotapi.Message{Sticker: &tgbotapi.Sticker{FileID: "sticker.webp", Emoji: "🙂", SetName: "funpack"}})
	if err != nil {
		t.Fatalf("normalizeIncomingContent error: %v", err)
	}
	if text != "" {
		t.Fatalf("expected no synthetic sticker text, got %q", text)
	}
	if len(parts) != 1 {
		t.Fatalf("expected image-only content part, got %#v", parts)
	}
	if parts[0].Type != messagecontent.TypeImage || parts[0].MIMEType != "image/png" {
		t.Fatalf("expected image part, got %#v", parts[0])
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[0].Data)
	if err != nil {
		t.Fatalf("decode normalized image: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(decoded)); err != nil {
		t.Fatalf("expected normalized png data, got err=%v", err)
	}
}

func TestConvertStickerToPNGRejectsUndecodableSticker(t *testing.T) {
	_, err := convertStickerToPNG([]byte("not-an-image"))
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestNormalizeIncomingContentRejectsAnimatedSticker(t *testing.T) {
	b := &Bot{cfg: config.TelegramConfig{}}
	_, _, err := b.normalizeIncomingContent(&tgbotapi.Message{Sticker: &tgbotapi.Sticker{FileID: "s1", IsAnimated: true}})
	if err == nil || !strings.Contains(err.Error(), "animated") {
		t.Fatalf("expected animated sticker error, got %v", err)
	}
}

func TestStickerOnlyMessageSkipsThinkingParse(t *testing.T) {
	b := &Bot{cfg: config.TelegramConfig{}}
	if _, _, err := parseThinkingOverride(""); err == nil {
		t.Fatal("expected empty text to fail direct thinking parse precondition")
	}
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.NRGBA{G: 255, A: 255})
	var sticker bytes.Buffer
	if err := png.Encode(&sticker, img); err != nil {
		t.Fatalf("encode sticker png: %v", err)
	}
	b.api = func() *tgbotapi.BotAPI {
		api, err := tgbotapi.NewBotAPIWithClient("tok", "https://api.telegram.org/bot%s/%s", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "/getMe") {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"bot","username":"bot"}}`))}, nil
			}
			if strings.Contains(req.URL.String(), "/getFile") {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{"file_path":"sticker.webp"}}`))}, nil
			}
			return nil, errors.New("unexpected telegram api request")
		})})
		if err != nil {
			t.Fatalf("new bot api: %v", err)
		}
		return api
	}()
	b.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/webp"}}, Body: io.NopCloser(bytes.NewReader(sticker.Bytes()))}, nil
	})}
	content, parts, err := b.normalizeIncomingContent(&tgbotapi.Message{Sticker: &tgbotapi.Sticker{FileID: "sticker.webp"}})
	if err != nil {
		t.Fatalf("normalizeIncomingContent error: %v", err)
	}
	if content != "" || len(parts) != 1 {
		t.Fatalf("expected image-only sticker content, got content=%q parts=%#v", content, parts)
	}
}
