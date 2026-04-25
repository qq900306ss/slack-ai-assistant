package agent

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/slack-go/slack"

	"github.com/qq900306ss/slack-ai-assistant/internal/config"
	slackclient "github.com/qq900306ss/slack-ai-assistant/internal/slack"
)

// BotHandler handles bot mentions and responds using the agent.
type BotHandler struct {
	agent    *Agent
	client   *slackclient.Client
	sessions *SessionManager
	botID    string
	logger   *slog.Logger
}

// NewBotHandler creates a new bot handler.
func NewBotHandler(pool *pgxpool.Pool, client *slackclient.Client, cfg *config.Config, logger *slog.Logger) (*BotHandler, error) {
	agent, err := NewAgent(pool, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	// Get bot's user ID
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	botID, err := client.GetBotUserID(ctx)
	if err != nil {
		return nil, fmt.Errorf("get bot user id: %w", err)
	}

	logger.Info("bot handler initialized", "bot_id", botID)

	return &BotHandler{
		agent:    agent,
		client:   client,
		sessions: NewSessionManager(30 * time.Minute), // 30 min session TTL
		botID:    botID,
		logger:   logger,
	}, nil
}

// HandleMessage checks if the bot was mentioned and responds.
// Returns true if the bot handled the message (was mentioned).
func (h *BotHandler) HandleMessage(ctx context.Context, ev *slack.MessageEvent) (bool, error) {
	// Ignore bot messages to prevent loops
	if ev.BotID != "" || ev.User == "" {
		return false, nil
	}

	// Ignore message subtypes (edits, deletes, etc.)
	if ev.SubType != "" && ev.SubType != "thread_broadcast" {
		return false, nil
	}

	// Check if bot was mentioned
	if !h.isBotMentioned(ev.Text) {
		return false, nil
	}

	h.logger.Info("bot mentioned", "channel", ev.Channel, "user", ev.User)

	// Extract the actual question (remove bot mention)
	query := h.extractQuery(ev.Text)
	if query == "" {
		return true, nil // Mentioned but no query
	}

	// Determine thread TS (for session key and reply)
	threadTS := ev.ThreadTimestamp
	if threadTS == "" {
		threadTS = ev.Timestamp // Start new thread from this message
	}

	// Get or create session
	sessionKey := SessionKey(ev.Channel, threadTS)
	history := h.sessions.GetHistory(sessionKey)

	// Call agent
	response, newHistory, err := h.agent.Chat(ctx, history, query)
	if err != nil {
		h.logger.Error("agent chat failed", "error", err)
		response = fmt.Sprintf("抱歉，處理您的請求時發生錯誤：%v", err)
		// Don't update history on error
	} else {
		h.sessions.UpdateHistory(sessionKey, newHistory)
	}

	// Reply in thread
	_, err = h.client.PostMessage(ctx, ev.Channel, response, threadTS)
	if err != nil {
		h.logger.Error("failed to post reply", "error", err)
		return true, err
	}

	return true, nil
}

// isBotMentioned checks if the bot was mentioned in the message.
func (h *BotHandler) isBotMentioned(text string) bool {
	// Slack mentions look like <@U12345678>
	mention := fmt.Sprintf("<@%s>", h.botID)
	return strings.Contains(text, mention)
}

// extractQuery removes the bot mention and returns the actual query.
func (h *BotHandler) extractQuery(text string) string {
	// Remove bot mention
	mention := fmt.Sprintf("<@%s>", h.botID)
	query := strings.ReplaceAll(text, mention, "")

	// Also remove any other user mentions from the query (keep them as context but clean up)
	// But actually, let's keep other mentions as they might be relevant to the query

	// Remove extra whitespace
	query = strings.TrimSpace(query)

	// Remove common prefixes/suffixes
	query = strings.TrimPrefix(query, ":")
	query = strings.TrimSpace(query)

	return query
}

// BotID returns the bot's user ID.
func (h *BotHandler) BotID() string {
	return h.botID
}

// ClearSession clears the conversation history for a thread.
// Can be triggered by commands like "clear" or "reset".
func (h *BotHandler) ClearSession(channelID, threadTS string) {
	h.sessions.ClearSession(SessionKey(channelID, threadTS))
}

// mentionRegex matches Slack user mentions.
var mentionRegex = regexp.MustCompile(`<@[A-Z0-9]+>`)
