package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	bedrockdocument "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	bedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/H4fizWasabie/yen/internal/agent"
)

// BedrockConverse implements the AWS credential-chain ConverseStream API.
type BedrockConverse struct {
	Region       string
	Profile      string
	BaseURL      string
	Model        string
	ProviderName string
	Client       *bedrockruntime.Client
}

func NewBedrockConverse(region, model string) BedrockConverse {
	return BedrockConverse{Region: region, Model: model, ProviderName: "amazon-bedrock"}
}

func (p BedrockConverse) Next(ctx context.Context, messages []agent.Message, tools []string) (agent.Response, error) {
	return p.next(ctx, messages, tools, nil)
}

func (p BedrockConverse) NextWithEvents(ctx context.Context, messages []agent.Message, tools []string, emit func(agent.StreamEvent)) (agent.Response, error) {
	return p.next(ctx, messages, tools, emit)
}

func (p BedrockConverse) NextWithUpdates(ctx context.Context, messages []agent.Message, tools []string, update func(string)) (agent.Response, error) {
	return p.next(ctx, messages, tools, func(event agent.StreamEvent) {
		if event.Type == "text_delta" && update != nil {
			update(event.Delta)
		}
	})
}

func (p BedrockConverse) next(ctx context.Context, messages []agent.Message, toolNames []string, emit func(agent.StreamEvent)) (agent.Response, error) {
	if strings.TrimSpace(p.Model) == "" {
		return agent.Response{}, errors.New("amazon bedrock model is required")
	}
	client := p.Client
	if client == nil {
		region := p.Region
		if region == "" {
			region = os.Getenv("AWS_REGION")
		}
		if region == "" {
			region = "us-east-1"
		}
		cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region), awsconfig.WithSharedConfigProfile(p.Profile))
		if err != nil {
			return agent.Response{}, err
		}
		if p.BaseURL != "" {
			cfg.BaseEndpoint = aws.String(p.BaseURL)
		}
		client = bedrockruntime.NewFromConfig(cfg)
	}
	in, err := bedrockInput(messages, toolNames, p.Model)
	if err != nil {
		return agent.Response{}, err
	}
	out, err := client.ConverseStream(ctx, in)
	if err != nil {
		return agent.Response{}, err
	}
	providerName := p.ProviderName
	if providerName == "" {
		providerName = "amazon-bedrock"
	}
	result := agent.Response{Provider: providerName, Model: p.Model}
	partial := agent.Message{Role: "assistant", Provider: providerName, Model: p.Model}
	if emit != nil {
		emit(agent.StreamEvent{Type: "start", Partial: partial})
	}
	toolArgs := make(map[int]string)
	toolCalls := make(map[int]agent.ToolCall)
	for event := range out.GetStream().Events() {
		switch event := event.(type) {
		case *bedrocktypes.ConverseStreamOutputMemberContentBlockStart:
			if start, ok := event.Value.Start.(*bedrocktypes.ContentBlockStartMemberToolUse); ok {
				call := agent.ToolCall{ID: stringValue(start.Value.ToolUseId), Name: stringValue(start.Value.Name)}
				index := int(aws.ToInt32(event.Value.ContentBlockIndex))
				toolCalls[index] = call
				if emit != nil {
					emit(agent.StreamEvent{Type: "toolcall_start", ContentIndex: index, ToolCall: &call, Partial: partial})
				}
			}
		case *bedrocktypes.ConverseStreamOutputMemberContentBlockDelta:
			index := int(aws.ToInt32(event.Value.ContentBlockIndex))
			switch delta := event.Value.Delta.(type) {
			case *bedrocktypes.ContentBlockDeltaMemberText:
				result.Text += delta.Value
				partial.Content += delta.Value
				if emit != nil {
					emit(agent.StreamEvent{Type: "text_delta", ContentIndex: index, Delta: delta.Value, Partial: partial})
				}
			case *bedrocktypes.ContentBlockDeltaMemberToolUse:
				toolArgs[index] += stringValue(delta.Value.Input)
				if emit != nil {
					emit(agent.StreamEvent{Type: "toolcall_delta", ContentIndex: index, Delta: stringValue(delta.Value.Input), Partial: partial})
				}
			case *bedrocktypes.ContentBlockDeltaMemberReasoningContent:
				if text, ok := delta.Value.(*bedrocktypes.ReasoningContentBlockDeltaMemberText); ok {
					result.Thinking += text.Value
					partial.Thinking += text.Value
					if emit != nil {
						emit(agent.StreamEvent{Type: "thinking_delta", ContentIndex: index, Delta: text.Value, Partial: partial})
					}
				}
			}
		case *bedrocktypes.ConverseStreamOutputMemberMessageStop:
			result.StopReason = bedrockStopReason(string(event.Value.StopReason))
		case *bedrocktypes.ConverseStreamOutputMemberMetadata:
			if usage := event.Value.Usage; usage != nil {
				result.Usage.Input = int(aws.ToInt32(usage.InputTokens))
				result.Usage.Output = int(aws.ToInt32(usage.OutputTokens))
				result.Usage.TotalTokens = int(aws.ToInt32(usage.TotalTokens))
			}
		}
	}
	if err := out.GetStream().Err(); err != nil {
		return agent.Response{}, err
	}
	for index, call := range toolCalls {
		if err := json.Unmarshal([]byte(toolArgs[index]), &call.Args); err != nil {
			return agent.Response{}, fmt.Errorf("bedrock tool %s arguments: %w", call.Name, err)
		}
		result.ToolCalls = append(result.ToolCalls, call)
		partial.ToolCalls = append(partial.ToolCalls, call)
		if emit != nil {
			emit(agent.StreamEvent{Type: "toolcall_end", ContentIndex: index, ToolCall: &call, Partial: partial})
		}
	}
	if result.StopReason == "" {
		return agent.Response{}, errors.New("amazon bedrock stream ended without a stop reason")
	}
	if emit != nil {
		emit(agent.StreamEvent{Type: "done", Partial: partial})
	}
	return result, nil
}

func bedrockInput(messages []agent.Message, toolNames []string, model string) (*bedrockruntime.ConverseStreamInput, error) {
	input := &bedrockruntime.ConverseStreamInput{ModelId: aws.String(model)}
	for _, message := range messages {
		if message.Role == "system" {
			if strings.TrimSpace(message.Content) != "" {
				input.System = append(input.System, &bedrocktypes.SystemContentBlockMemberText{Value: message.Content})
			}
			continue
		}
		role := "user"
		if message.Role == "assistant" {
			role = "assistant"
		}
		if strings.TrimSpace(message.Content) == "" {
			continue
		}
		input.Messages = append(input.Messages, bedrocktypes.Message{Role: bedrocktypes.ConversationRole(role), Content: []bedrocktypes.ContentBlock{&bedrocktypes.ContentBlockMemberText{Value: message.Content}}})
	}
	if len(input.Messages) == 0 {
		return nil, errors.New("amazon bedrock requires a user message")
	}
	if len(toolNames) > 0 {
		input.ToolConfig = &bedrocktypes.ToolConfiguration{}
		for _, name := range toolNames {
			input.ToolConfig.Tools = append(input.ToolConfig.Tools, &bedrocktypes.ToolMemberToolSpec{Value: bedrocktypes.ToolSpecification{Name: aws.String(name), InputSchema: &bedrocktypes.ToolInputSchemaMemberJson{Value: bedrockdocument.NewLazyDocument(map[string]any{"type": "object"})}}})
		}
	}
	return input, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func bedrockStopReason(reason string) string {
	switch reason {
	case "end_turn", "stop_sequence":
		return "stop"
	case "tool_use":
		return "tool_use"
	case "max_tokens":
		return "length"
	default:
		return reason
	}
}

var _ agent.Provider = BedrockConverse{}
var _ agent.StreamingProviderWithEvents = BedrockConverse{}
