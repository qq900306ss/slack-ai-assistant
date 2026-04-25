package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/sashabaranov/go-openai"

	"github.com/qq900306ss/slack-ai-assistant/internal/config"
	"github.com/qq900306ss/slack-ai-assistant/internal/db"
	"github.com/qq900306ss/slack-ai-assistant/internal/retrieval"
	"github.com/jackc/pgx/v5/pgxpool"
)

const systemPrompt = `You are a helpful AI assistant that can search and analyze Slack messages.

Your capabilities:
1. Search messages by keywords or semantic meaning
2. Retrieve full conversation threads
3. Look up user information
4. List available channels

When answering questions:
- Always cite your sources with Slack permalinks
- If you can't find relevant information, say so honestly
- Be concise but complete
- Use the search tool to find relevant messages before answering

If the user asks about something that happened in Slack, use the search_messages tool first.
If you need more context about a conversation, use get_thread to see the full discussion.`

// Agent handles conversations with OpenAI
type Agent struct {
	client   *openai.Client
	executor *ToolExecutor
	logger   *slog.Logger
	model    string
}

// NewAgent creates a new agent
func NewAgent(pool *pgxpool.Pool, cfg *config.Config, logger *slog.Logger) (*Agent, error) {
	if cfg.OpenAIAPIKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required")
	}

	client := openai.NewClient(cfg.OpenAIAPIKey)
	retrievalClient := retrieval.NewClient(pool, cfg, logger)
	queries := db.New(pool)

	return &Agent{
		client:   client,
		executor: NewToolExecutor(retrievalClient, queries),
		logger:   logger,
		model:    "gpt-4o-mini",
	}, nil
}

// Message represents a conversation message
type Message struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content string `json:"content"`
}

// Chat processes a user message and returns the assistant's response
func (a *Agent) Chat(ctx context.Context, history []Message, userMessage string) (string, []Message, error) {
	// Build message history for API
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
	}

	for _, m := range history {
		role := openai.ChatMessageRoleUser
		if m.Role == "assistant" {
			role = openai.ChatMessageRoleAssistant
		}
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    role,
			Content: m.Content,
		})
	}

	// Add current user message
	messages = append(messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: userMessage,
	})

	// Update history
	newHistory := append(history, Message{Role: "user", Content: userMessage})

	// Agent loop
	for iterations := 0; iterations < 10; iterations++ {
		resp, err := a.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:    a.model,
			Messages: messages,
			Tools:    AllTools(),
		})
		if err != nil {
			return "", nil, fmt.Errorf("openai api error: %w", err)
		}

		if len(resp.Choices) == 0 {
			return "", nil, fmt.Errorf("no response from openai")
		}

		choice := resp.Choices[0]
		assistantMsg := choice.Message

		// Add assistant message to conversation
		messages = append(messages, assistantMsg)

		// If no tool calls, we're done
		if len(assistantMsg.ToolCalls) == 0 {
			newHistory = append(newHistory, Message{Role: "assistant", Content: assistantMsg.Content})
			return assistantMsg.Content, newHistory, nil
		}

		// Execute tools
		for _, tc := range assistantMsg.ToolCalls {
			a.logger.Debug("executing tool", "name", tc.Function.Name, "input", tc.Function.Arguments)

			result, err := a.executor.Execute(ctx, tc.Function.Name, json.RawMessage(tc.Function.Arguments))
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
			}

			// Add tool result
			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}

	return "", nil, fmt.Errorf("max iterations reached")
}
