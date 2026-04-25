package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sashabaranov/go-openai"

	"github.com/qq900306ss/slack-ai-assistant/internal/db"
	"github.com/qq900306ss/slack-ai-assistant/internal/retrieval"
)

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

// AllTools returns all available tools
func AllTools() []openai.Tool {
	return []openai.Tool{
		SearchMessagesTool(),
		GetThreadTool(),
		ListChannelsTool(),
		GetUserTool(),
		GetRecentMessagesTool(),
	}
}

// ToolExecutor handles tool execution
type ToolExecutor struct {
	retrieval *retrieval.Client
	queries   *db.Queries
}

// NewToolExecutor creates a new tool executor
func NewToolExecutor(retrieval *retrieval.Client, queries *db.Queries) *ToolExecutor {
	return &ToolExecutor{
		retrieval: retrieval,
		queries:   queries,
	}
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

	return formatSearchResults(results), nil
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
