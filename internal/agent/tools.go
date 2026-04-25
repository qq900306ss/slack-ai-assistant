package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/sashabaranov/go-openai"

	"github.com/qq900306ss/slack-ai-assistant/internal/db"
	"github.com/qq900306ss/slack-ai-assistant/internal/retrieval"
)

var userMentionRegex = regexp.MustCompile(`<@([A-Z0-9]+)>`)

// Tool definitions for OpenAI

func SearchMessagesTool() openai.Tool {
	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "search_messages",
			Description: "Search Slack messages by query. Returns relevant messages with content, author, channel, and permalink.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "The search query - can be keywords or natural language describing what you're looking for",
					},
					"channel": map[string]any{
						"type":        "string",
						"description": "Optional: filter by channel name (e.g., 'general', 'engineering')",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 10, max: 20)",
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

func GetThreadTool() openai.Tool {
	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "get_thread",
			Description: "Get all messages in a Slack thread. Use this to see the full conversation context.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel_id": map[string]any{
						"type":        "string",
						"description": "The Slack channel ID (e.g., 'C01234567')",
					},
					"thread_ts": map[string]any{
						"type":        "string",
						"description": "The thread timestamp (e.g., '1234567890.123456')",
					},
				},
				"required": []string{"channel_id", "thread_ts"},
			},
		},
	}
}

func ListChannelsTool() openai.Tool {
	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "list_channels",
			Description: "List available Slack channels. Use this to discover channel names before searching.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"include_private": map[string]any{
						"type":        "boolean",
						"description": "Whether to include private channels (default: false)",
					},
				},
				"required": []string{},
			},
		},
	}
}

func GetUserTool() openai.Tool {
	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "get_user",
			Description: "Get information about a Slack user by their ID or name.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"user_id": map[string]any{
						"type":        "string",
						"description": "The Slack user ID (e.g., 'U01234567') or username",
					},
				},
				"required": []string{"user_id"},
			},
		},
	}
}

func GetRecentMessagesTool() openai.Tool {
	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "get_recent_messages",
			Description: "Get the most recent messages from a specific channel or group. Use this to see what's been discussed lately in a channel.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel_id": map[string]any{
						"type":        "string",
						"description": "The Slack channel ID (e.g., 'C01234567')",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of messages to return (default: 20, max: 50)",
					},
				},
				"required": []string{"channel_id"},
			},
		},
	}
}

func GetUserMessagesTool() openai.Tool {
	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "get_user_messages",
			Description: "Get recent messages from a specific user. Use this to analyze someone's communication style, topics they discuss, or their activity patterns.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"username": map[string]any{
						"type":        "string",
						"description": "The username or display name to search for (e.g., 'sherry', 'david')",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of messages to return (default: 30, max: 50)",
					},
				},
				"required": []string{"username"},
			},
		},
	}
}

func ExpandContextTool() openai.Tool {
	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "expand_context",
			Description: "Get more surrounding messages around a specific message. Use this when the user wants more context about a conversation or says things like '給我更多上下文', 'show me more', 'expand this'.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"channel_id": map[string]any{
						"type":        "string",
						"description": "The Slack channel ID",
					},
					"slack_ts": map[string]any{
						"type":        "string",
						"description": "The message timestamp to expand context around",
					},
					"count": map[string]any{
						"type":        "integer",
						"description": "Number of messages before and after to fetch (default: 10, max: 20)",
					},
				},
				"required": []string{"channel_id", "slack_ts"},
			},
		},
	}
}

func GetUserMentionsTool() openai.Tool {
	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "get_user_mentions",
			Description: "Get messages where a specific user was @mentioned (tagged). Use this to find conversations where someone was called out or asked for help. This is different from get_user_messages which shows what the user said.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"username": map[string]any{
						"type":        "string",
						"description": "The username or display name to find mentions for (e.g., 'ray', 'kevin')",
					},
					"public_only": map[string]any{
						"type":        "boolean",
						"description": "If true, only search public channels. If user asks for '公開頻道' or 'public channels', set this to true.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 20, max: 50)",
					},
				},
				"required": []string{"username"},
			},
		},
	}
}

func ListTeamMembersTool() openai.Tool {
	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        "list_team_members",
			Description: "List all team members (non-bot users) who have sent messages recently. Use this to discover who is on the team before analyzing multiple people.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of members to return (default: 30)",
					},
				},
				"required": []string{},
			},
		},
	}
}

// AllTools returns all available tools
func AllTools() []openai.Tool {
	return []openai.Tool{
		SearchMessagesTool(),
		GetThreadTool(),
		ListChannelsTool(),
		GetUserTool(),
		GetRecentMessagesTool(),
		GetUserMessagesTool(),
		ExpandContextTool(),
		GetUserMentionsTool(),
		ListTeamMembersTool(),
	}
}

// ToolExecutor handles tool execution
type ToolExecutor struct {
	retrieval *retrieval.Client
	queries   *db.Queries
	userCache map[string]string // userID -> displayName
}

// NewToolExecutor creates a new tool executor
func NewToolExecutor(retrieval *retrieval.Client, queries *db.Queries) *ToolExecutor {
	return &ToolExecutor{
		retrieval: retrieval,
		queries:   queries,
		userCache: make(map[string]string),
	}
}

// resolveUserMentions replaces <@USER_ID> with @username in text
func (e *ToolExecutor) resolveUserMentions(ctx context.Context, text string) string {
	// Build cache if empty
	if len(e.userCache) == 0 {
		users, err := e.queries.ListUsers(ctx)
		if err == nil {
			for _, u := range users {
				name := db.TextValue(u.DisplayName)
				if name == "" {
					name = db.TextValue(u.Name)
				}
				if name != "" {
					e.userCache[u.ID] = name
				}
			}
		}
	}

	return userMentionRegex.ReplaceAllStringFunc(text, func(match string) string {
		// Extract user ID from <@USER_ID>
		userID := match[2 : len(match)-1]
		if name, ok := e.userCache[userID]; ok {
			return "@" + name
		}
		return match // Keep original if not found
	})
}

// Execute runs a tool and returns the result as a string
func (e *ToolExecutor) Execute(ctx context.Context, toolName string, input json.RawMessage) (string, error) {
	switch toolName {
	case "search_messages":
		return e.searchMessages(ctx, input)
	case "get_thread":
		return e.getThread(ctx, input)
	case "list_channels":
		return e.listChannels(ctx, input)
	case "get_user":
		return e.getUser(ctx, input)
	case "get_recent_messages":
		return e.getRecentMessages(ctx, input)
	case "get_user_messages":
		return e.getUserMessages(ctx, input)
	case "expand_context":
		return e.expandContext(ctx, input)
	case "get_user_mentions":
		return e.getUserMentions(ctx, input)
	case "list_team_members":
		return e.listTeamMembers(ctx, input)
	default:
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}
}

func (e *ToolExecutor) searchMessages(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Query   string `json:"query"`
		Channel string `json:"channel"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", err
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 20 {
		limit = 20
	}

	filter := retrieval.SearchFilter{Limit: limit}

	// If channel name specified, look up channel ID
	if params.Channel != "" {
		channels, err := e.queries.ListChannels(ctx)
		if err == nil {
			for _, ch := range channels {
				if db.TextValue(ch.Name) == params.Channel {
					filter.ChannelIDs = []string{ch.ID}
					break
				}
			}
		}
	}

	results, err := e.retrieval.Search(ctx, params.Query, filter)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "No messages found matching your query.", nil
	}

	// Fetch thread context for top results (up to 5)
	return e.formatSearchResultsWithContext(ctx, results), nil
}

// formatSearchResultsWithContext fetches thread context for each result and formats them.
func (e *ToolExecutor) formatSearchResultsWithContext(ctx context.Context, results []retrieval.SearchResult) string {
	var out string
	seen := make(map[string]bool) // Track seen threads to avoid duplicates

	maxWithContext := 5
	if len(results) < maxWithContext {
		maxWithContext = len(results)
	}

	for i, r := range results[:maxWithContext] {
		// Determine thread key
		threadKey := r.ChannelID + ":" + r.SlackTS
		if r.ThreadTS != "" {
			threadKey = r.ChannelID + ":" + r.ThreadTS
		}

		if seen[threadKey] {
			continue
		}
		seen[threadKey] = true

		out += fmt.Sprintf("=== 對話 %d ===\n", i+1)
		out += fmt.Sprintf("頻道: #%s\n", r.ChannelName)
		out += fmt.Sprintf("連結: %s\n\n", r.Permalink)

		// Try to get context (thread first, then surrounding messages)
		var contextMsgs []retrieval.SearchResult
		var err error

		if r.ThreadTS != "" {
			contextMsgs, err = e.retrieval.GetThread(ctx, r.ChannelID, r.ThreadTS)
		}

		if err != nil || len(contextMsgs) <= 1 {
			// No thread, get surrounding messages
			contextMsgs, err = e.retrieval.GetSurroundingMessages(ctx, r.ChannelID, r.SlackTS, 4)
		}

		if err != nil || len(contextMsgs) == 0 {
			// Still nothing, show single message
			out += fmt.Sprintf("[@%s %s]\n%s\n\n", r.UserName, r.CreatedAt.Format("01/02 15:04"), r.Text)
		} else {
			// Show context
			for _, msg := range contextMsgs {
				text := msg.Text
				if len(text) > 300 {
					text = text[:300] + "..."
				}
				out += fmt.Sprintf("[@%s %s]\n%s\n\n", msg.UserName, msg.CreatedAt.Format("01/02 15:04"), text)
			}
		}
	}

	return out
}

func (e *ToolExecutor) getThread(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		ChannelID string `json:"channel_id"`
		ThreadTS  string `json:"thread_ts"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", err
	}

	results, err := e.retrieval.GetThread(ctx, params.ChannelID, params.ThreadTS)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "Thread not found.", nil
	}

	return formatThreadResults(results), nil
}

func (e *ToolExecutor) listChannels(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		IncludePrivate bool `json:"include_private"`
	}
	json.Unmarshal(input, &params) // Ignore error, use defaults

	channels, err := e.queries.ListChannels(ctx)
	if err != nil {
		return "", err
	}

	var result string
	for _, ch := range channels {
		if !params.IncludePrivate && ch.IsPrivate {
			continue
		}
		name := db.TextValue(ch.Name)
		if name == "" {
			continue
		}
		visibility := "public"
		if ch.IsPrivate {
			visibility = "private"
		}
		result += fmt.Sprintf("- #%s (%s, ID: %s)\n", name, visibility, ch.ID)
	}

	if result == "" {
		return "No channels found.", nil
	}

	return result, nil
}

func (e *ToolExecutor) getUser(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", err
	}

	user, err := e.queries.GetUser(ctx, params.UserID)
	if err != nil {
		return fmt.Sprintf("User not found: %s", params.UserID), nil
	}

	return fmt.Sprintf("User: %s\nDisplay Name: %s\nReal Name: %s\nIs Bot: %v",
		db.TextValue(user.Name),
		db.TextValue(user.DisplayName),
		db.TextValue(user.RealName),
		user.IsBot,
	), nil
}

func (e *ToolExecutor) getRecentMessages(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		ChannelID string `json:"channel_id"`
		Limit     int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", err
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	results, err := e.retrieval.GetRecentMessages(ctx, params.ChannelID, limit)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "No messages found in this channel.", nil
	}

	return formatRecentMessages(results), nil
}

func (e *ToolExecutor) getUserMessages(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Username string `json:"username"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", err
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 30
	}
	if limit > 50 {
		limit = 50
	}

	results, err := e.retrieval.GetUserMessages(ctx, params.Username, limit)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return fmt.Sprintf("No messages found from user matching '%s'.", params.Username), nil
	}

	return e.formatUserMessagesWithContext(ctx, results, params.Username), nil
}

// formatUserMessagesWithContext fetches thread context for user messages.
func (e *ToolExecutor) formatUserMessagesWithContext(ctx context.Context, results []retrieval.SearchResult, username string) string {
	var out string
	out += fmt.Sprintf("Found %d messages from @%s (with context):\n\n", len(results), username)

	seen := make(map[string]bool)
	maxWithContext := 8
	if len(results) < maxWithContext {
		maxWithContext = len(results)
	}

	for i, r := range results[:maxWithContext] {
		// Determine thread key
		threadKey := r.ChannelID + ":" + r.SlackTS
		if r.ThreadTS != "" {
			threadKey = r.ChannelID + ":" + r.ThreadTS
		}

		if seen[threadKey] {
			continue
		}
		seen[threadKey] = true

		out += fmt.Sprintf("=== 對話 %d (#%s %s) ===\n", i+1, r.ChannelName, r.CreatedAt.Format("01/02 15:04"))

		// Try to get thread context first
		var contextMsgs []retrieval.SearchResult
		var err error

		if r.ThreadTS != "" {
			// Message is part of a thread
			contextMsgs, err = e.retrieval.GetThread(ctx, r.ChannelID, r.ThreadTS)
		}

		if err != nil || len(contextMsgs) <= 1 {
			// No thread or thread fetch failed, get surrounding messages
			contextMsgs, err = e.retrieval.GetSurroundingMessages(ctx, r.ChannelID, r.SlackTS, 3)
		}

		if err != nil || len(contextMsgs) == 0 {
			// Still nothing, just show the single message
			out += fmt.Sprintf("[@%s] %s\n\n", r.UserName, r.Text)
		} else {
			// Show context messages
			for _, msg := range contextMsgs {
				text := msg.Text
				if len(text) > 200 {
					text = text[:200] + "..."
				}
				out += fmt.Sprintf("[@%s] %s\n", msg.UserName, text)
			}
			out += "\n"
		}
	}

	return out
}

func (e *ToolExecutor) expandContext(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		ChannelID string `json:"channel_id"`
		SlackTS   string `json:"slack_ts"`
		Count     int    `json:"count"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", err
	}

	count := params.Count
	if count <= 0 {
		count = 10
	}
	if count > 20 {
		count = 20
	}

	results, err := e.retrieval.GetSurroundingMessages(ctx, params.ChannelID, params.SlackTS, count)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "No messages found around this timestamp.", nil
	}

	var out string
	out += fmt.Sprintf("擴展上下文 (共 %d 則訊息):\n\n", len(results))

	for _, r := range results {
		text := r.Text
		if len(text) > 400 {
			text = text[:400] + "..."
		}
		out += fmt.Sprintf("[@%s %s]\n%s\n\n", r.UserName, r.CreatedAt.Format("01/02 15:04"), text)
	}

	return out, nil
}

func formatRecentMessages(results []retrieval.SearchResult) string {
	var out string
	out += fmt.Sprintf("Recent %d messages:\n\n", len(results))

	for _, r := range results {
		text := r.Text
		if len(text) > 300 {
			text = text[:300] + "..."
		}
		out += fmt.Sprintf("[@%s %s]\n%s\n\n", r.UserName, r.CreatedAt.Format("2006-01-02 15:04"), text)
	}

	return out
}

func formatSearchResults(results []retrieval.SearchResult) string {
	var out string
	for i, r := range results {
		text := r.Text
		if len(text) > 300 {
			text = text[:300] + "..."
		}

		out += fmt.Sprintf("**Result %d**\n", i+1)
		out += fmt.Sprintf("- Channel: #%s\n", r.ChannelName)
		out += fmt.Sprintf("- Author: @%s\n", r.UserName)
		out += fmt.Sprintf("- Time: %s\n", r.CreatedAt.Format(time.RFC3339))
		out += fmt.Sprintf("- Permalink: %s\n", r.Permalink)
		out += fmt.Sprintf("- Content: %s\n\n", text)
	}
	return out
}

func formatThreadResults(results []retrieval.SearchResult) string {
	var out string
	out += fmt.Sprintf("Thread with %d messages:\n\n", len(results))

	for _, r := range results {
		text := r.Text
		if len(text) > 500 {
			text = text[:500] + "..."
		}
		out += fmt.Sprintf("[@%s %s]\n%s\n\n", r.UserName, r.CreatedAt.Format("2006-01-02 15:04"), text)
	}

	if len(results) > 0 {
		out += fmt.Sprintf("Permalink: %s\n", results[0].Permalink)
	}

	return out
}

func (e *ToolExecutor) getUserMentions(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Username   string `json:"username"`
		PublicOnly bool   `json:"public_only"`
		Limit      int    `json:"limit"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", err
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	// First, find the user ID from username
	users, err := e.queries.ListUsers(ctx)
	if err != nil {
		return "", err
	}

	// Find best match: prefer exact match, then display_name contains, then name contains
	var userID, matchedName string
	searchPattern := params.Username

	// Pass 1: exact match on display_name or name
	for _, u := range users {
		name := db.TextValue(u.Name)
		displayName := db.TextValue(u.DisplayName)
		if equalIgnoreCase(displayName, searchPattern) || equalIgnoreCase(name, searchPattern) {
			userID = u.ID
			matchedName = displayName
			if matchedName == "" {
				matchedName = name
			}
			break
		}
	}

	// Pass 2: display_name contains (prefer display_name over username)
	if userID == "" {
		for _, u := range users {
			displayName := db.TextValue(u.DisplayName)
			if displayName != "" && containsIgnoreCase(displayName, searchPattern) {
				userID = u.ID
				matchedName = displayName
				break
			}
		}
	}

	// Pass 3: name contains
	if userID == "" {
		for _, u := range users {
			name := db.TextValue(u.Name)
			if containsIgnoreCase(name, searchPattern) {
				userID = u.ID
				matchedName = name
				break
			}
		}
	}

	if userID == "" {
		return fmt.Sprintf("User '%s' not found.", params.Username), nil
	}

	// Search for mentions using <@USER_ID> format
	results, err := e.retrieval.GetUserMentions(ctx, userID, limit, params.PublicOnly)
	if err != nil {
		return "", err
	}

	channelType := ""
	if params.PublicOnly {
		channelType = " (僅公開頻道)"
	}

	if len(results) == 0 {
		return fmt.Sprintf("No messages found mentioning @%s%s.", matchedName, channelType), nil
	}

	return e.formatMentionsWithContext(ctx, results, matchedName) + channelType, nil
}

func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(substr) > 0 && findIgnoreCase(s, substr)))
}

func findIgnoreCase(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if equalIgnoreCase(s[i:i+len(substr)], substr) {
			return true
		}
	}
	return false
}

func equalIgnoreCase(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

func (e *ToolExecutor) formatMentionsWithContext(ctx context.Context, results []retrieval.SearchResult, username string) string {
	var out string
	out += fmt.Sprintf("找到 %d 則提及 @%s 的訊息 (按時間由新到舊):\n\n", len(results), username)

	seen := make(map[string]bool)
	maxWithContext := 10
	if len(results) < maxWithContext {
		maxWithContext = len(results)
	}

	count := 0
	for _, r := range results {
		if count >= maxWithContext {
			break
		}

		// Determine thread key
		threadKey := r.ChannelID + ":" + r.SlackTS
		if r.ThreadTS != "" {
			threadKey = r.ChannelID + ":" + r.ThreadTS
		}

		if seen[threadKey] {
			continue
		}
		seen[threadKey] = true
		count++

		// Show clear header with who mentioned and when
		out += fmt.Sprintf("=== %d. #%s (%s) ===\n", count, r.ChannelName, r.CreatedAt.Format("01/02 15:04"))
		out += fmt.Sprintf("發送者: @%s\n", r.UserName)
		out += fmt.Sprintf("連結: %s\n", r.Permalink)

		// Show the mention message first (truncated if needed)
		mentionText := e.resolveUserMentions(ctx, r.Text)
		if len(mentionText) > 400 {
			mentionText = mentionText[:400] + "..."
		}
		out += fmt.Sprintf("內容: %s\n", mentionText)

		// Try to get thread replies if exists
		if r.ThreadTS != "" {
			contextMsgs, err := e.retrieval.GetThread(ctx, r.ChannelID, r.ThreadTS)
			if err == nil && len(contextMsgs) > 1 {
				out += fmt.Sprintf("(共 %d 則回覆)\n", len(contextMsgs)-1)
				// Show first few replies
				replyCount := 0
				for _, msg := range contextMsgs {
					if msg.SlackTS == r.SlackTS {
						continue // Skip the original message
					}
					replyCount++
					if replyCount > 3 {
						out += "...(更多回覆請點連結查看)\n"
						break
					}
					replyText := e.resolveUserMentions(ctx, msg.Text)
					if len(replyText) > 150 {
						replyText = replyText[:150] + "..."
					}
					out += fmt.Sprintf("  └ @%s: %s\n", msg.UserName, replyText)
				}
			}
		}
		out += "\n"
	}

	return out
}

func (e *ToolExecutor) listTeamMembers(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Limit int `json:"limit"`
	}
	json.Unmarshal(input, &params)

	limit := params.Limit
	if limit <= 0 {
		limit = 30
	}

	members, err := e.retrieval.GetActiveTeamMembers(ctx, limit)
	if err != nil {
		return "", err
	}

	if len(members) == 0 {
		return "No active team members found.", nil
	}

	var out string
	out += fmt.Sprintf("團隊成員 (依最近活躍度排序, 共 %d 人):\n\n", len(members))
	for i, m := range members {
		out += fmt.Sprintf("%d. @%s", i+1, m.Name)
		if m.DisplayName != "" && m.DisplayName != m.Name {
			out += fmt.Sprintf(" (%s)", m.DisplayName)
		}
		out += fmt.Sprintf(" - 最近訊息: %s\n", m.LastMessageAt.Format("01/02"))
	}

	return out, nil
}
