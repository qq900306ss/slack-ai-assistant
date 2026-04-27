package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicClient wraps the Anthropic API client
type AnthropicClient struct {
	client *anthropic.Client
	model  string
}

// NewAnthropicClient creates a new Anthropic client
func NewAnthropicClient(apiKey, model string) *AnthropicClient {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &AnthropicClient{
		client: &client,
		model:  model,
	}
}

// anthropicTools converts our tools to Anthropic format
func anthropicTools() []anthropic.ToolUnionParam {
	openaiTools := AllTools()
	tools := make([]anthropic.ToolUnionParam, 0, len(openaiTools))

	for _, t := range openaiTools {
		// Convert OpenAI function parameters to Anthropic input schema
		var inputSchema anthropic.ToolInputSchemaParam
		if t.Function.Parameters != nil {
			// Marshal and unmarshal to convert types
			data, _ := json.Marshal(t.Function.Parameters)
			json.Unmarshal(data, &inputSchema)
		}

		tools = append(tools, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Function.Name,
				Description: anthropic.String(t.Function.Description),
				InputSchema: inputSchema,
			},
		})
	}

	return tools
}

// Chat sends a message and handles tool calls
func (c *AnthropicClient) Chat(ctx context.Context, executor *ToolExecutor, history []Message, userMessage string) (string, []Message, error) {
	// Build messages
	messages := make([]anthropic.MessageParam, 0, len(history)+1)

	for _, m := range history {
		if m.Role == "user" {
			messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		} else {
			messages = append(messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Content)))
		}
	}

	// Add current user message
	messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(userMessage)))

	// Update history
	newHistory := append(history, Message{Role: "user", Content: userMessage})

	// Agent loop
	for iterations := 0; iterations < 10; iterations++ {
		resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     c.model,
			MaxTokens: 4096,
			System: []anthropic.TextBlockParam{
				{Text: systemPrompt},
			},
			Messages: messages,
			Tools:    anthropicTools(),
		})
		if err != nil {
			return "", nil, fmt.Errorf("anthropic api error: %w", err)
		}

		// Check for tool use and convert content blocks
		var hasToolUse bool
		var textContent string
		var toolResults []anthropic.ContentBlockParamUnion
		var assistantContent []anthropic.ContentBlockParamUnion

		for _, block := range resp.Content {
			switch block.Type {
			case "text":
				textContent = block.Text
				assistantContent = append(assistantContent, anthropic.NewTextBlock(block.Text))
			case "tool_use":
				hasToolUse = true
				assistantContent = append(assistantContent, anthropic.ContentBlockParamUnion{
					OfToolUse: &anthropic.ToolUseBlockParam{
						ID:    block.ID,
						Name:  block.Name,
						Input: block.Input,
					},
				})

				// Execute the tool
				inputJSON, _ := json.Marshal(block.Input)
				result, err := executor.Execute(ctx, block.Name, inputJSON)
				if err != nil {
					result = fmt.Sprintf("Error: %v", err)
				}

				toolResults = append(toolResults, anthropic.NewToolResultBlock(block.ID, result, false))
			}
		}

		// If no tool calls, we're done
		if !hasToolUse {
			newHistory = append(newHistory, Message{Role: "assistant", Content: textContent})
			return textContent, newHistory, nil
		}

		// Add assistant response with tool use
		messages = append(messages, anthropic.MessageParam{
			Role:    anthropic.MessageParamRoleAssistant,
			Content: assistantContent,
		})

		// Add tool results
		messages = append(messages, anthropic.NewUserMessage(toolResults...))
	}

	return "", nil, fmt.Errorf("max iterations reached")
}
