package provider

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
	sdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	bedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

func TestBedrockInputSeparatesSystemPromptAndToolSchemas(t *testing.T) {
	input, err := bedrockInput([]agent.Message{
		{Role: "system", Content: "You are precise."},
		{Role: "user", Content: "Read this file."},
	}, []string{"read"}, "model-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(input.System) != 1 || len(input.Messages) != 1 || input.Messages[0].Role != bedrocktypes.ConversationRoleUser {
		t.Fatalf("system=%#v messages=%#v", input.System, input.Messages)
	}
	if len(input.ToolConfig.Tools) != 1 {
		t.Fatalf("tools=%#v", input.ToolConfig)
	}
	tool, ok := input.ToolConfig.Tools[0].(*bedrocktypes.ToolMemberToolSpec)
	if !ok || tool.Value.Name == nil || *tool.Value.Name != "read" {
		t.Fatalf("tool=%#v", input.ToolConfig.Tools[0])
	}
	if _, ok := tool.Value.InputSchema.(*bedrocktypes.ToolInputSchemaMemberJson); !ok {
		t.Fatalf("schema=%T", tool.Value.InputSchema)
	}
}

func TestBedrockUsagePreservesCacheTokenBreakdown(t *testing.T) {
	usage := bedrocktypes.TokenUsage{
		InputTokens: sdk.Int32(100), OutputTokens: sdk.Int32(12), TotalTokens: sdk.Int32(112),
		CacheReadInputTokens: sdk.Int32(30), CacheWriteInputTokens: sdk.Int32(8),
	}
	var response agent.Response
	applyBedrockUsage(&response, &usage)
	if response.Usage.Input != 100 || response.Usage.Output != 12 || response.Usage.CacheRead != 30 || response.Usage.CacheWrite != 8 || response.Usage.TotalTokens != 112 {
		t.Fatalf("usage=%#v", response.Usage)
	}
	usage.TotalTokens = nil
	applyBedrockUsage(&response, &usage)
	if response.Usage.TotalTokens != 150 {
		t.Fatalf("fallback total=%d, want 150", response.Usage.TotalTokens)
	}
}

func TestNewConfiguredSupportsAmazonBedrock(t *testing.T) {
	t.Setenv("AWS_REGION", "ap-southeast-1")
	t.Setenv("AWS_PROFILE", "yen")
	configured, err := NewConfigured("amazon-bedrock", "model-1")
	if err != nil {
		t.Fatal(err)
	}
	client, ok := configured.(BedrockConverse)
	if !ok || client.ProviderName != "amazon-bedrock" || client.Model != "model-1" || client.Region != "ap-southeast-1" || client.Profile != "yen" {
		t.Fatalf("configured=%#v", configured)
	}
	name, model := Describe(configured)
	if name != "amazon-bedrock" || model != "model-1" {
		t.Fatalf("provider=%q model=%q", name, model)
	}
}

func TestBedrockInputReplaysToolResultsImagesAndClaudeReasoning(t *testing.T) {
	input, err := bedrockInput([]agent.Message{
		{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1", Name: "read", Args: map[string]any{"path": "x"}}}, Thinking: "inspect", ThinkingSignature: "sig"},
		{Role: "tool", ToolCallID: "call-1", Content: "contents", Images: []string{"data:image/png;base64,AQ=="}},
	}, nil, "anthropic.claude-3-7-sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(input.Messages) != 2 {
		t.Fatalf("messages=%#v", input.Messages)
	}
	assistantReasoning, ok := input.Messages[0].Content[0].(*bedrocktypes.ContentBlockMemberReasoningContent)
	if !ok || assistantReasoning.Value == nil {
		t.Fatalf("assistant content=%#v", input.Messages[0].Content)
	}
	assistantTool, ok := input.Messages[0].Content[1].(*bedrocktypes.ContentBlockMemberToolUse)
	if !ok || assistantTool.Value.ToolUseId == nil || *assistantTool.Value.ToolUseId != "call-1" {
		t.Fatalf("tool content=%#v", input.Messages[0].Content)
	}
	toolResult, ok := input.Messages[1].Content[0].(*bedrocktypes.ContentBlockMemberToolResult)
	if !ok || toolResult.Value.ToolUseId == nil || *toolResult.Value.ToolUseId != "call-1" || len(toolResult.Value.Content) != 2 {
		t.Fatalf("tool result=%#v", input.Messages[1].Content)
	}
	image, ok := toolResult.Value.Content[1].(*bedrocktypes.ToolResultContentBlockMemberImage)
	if !ok || len(image.Value.Source.(*bedrocktypes.ImageSourceMemberBytes).Value) != 1 {
		t.Fatalf("image=%#v", toolResult.Value.Content[1])
	}
}

func TestBedrockInputReplaysRedactedReasoningBytes(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	input, err := bedrockInput([]agent.Message{{Role: "assistant", Thinking: "[Reasoning redacted]", ThinkingSignature: encoded, ToolCalls: []agent.ToolCall{{ID: "call-2", Name: "read", Args: map[string]any{"path": "x"}}}}}, nil, "openai.gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	block, ok := input.Messages[0].Content[0].(*bedrocktypes.ContentBlockMemberReasoningContent)
	if !ok {
		t.Fatalf("content=%#v", input.Messages[0].Content)
	}
	redacted, ok := block.Value.(*bedrocktypes.ReasoningContentBlockMemberRedactedContent)
	if !ok || string(redacted.Value) != string([]byte{1, 2, 3}) {
		t.Fatalf("reasoning=%#v", block.Value)
	}
	if tool, ok := input.Messages[0].Content[1].(*bedrocktypes.ContentBlockMemberToolUse); !ok || *tool.Value.ToolUseId != "call-2" {
		t.Fatalf("redacted reasoning dropped tool call: %#v", input.Messages[0].Content)
	}
}

func TestBedrockInputRejectsUnsupportedOrInvalidImages(t *testing.T) {
	for _, image := range []string{
		"data:image/bmp;base64,AQ==",
		"data:image/png;base64,not-base64",
		"not-an-image",
	} {
		t.Run(image, func(t *testing.T) {
			_, err := bedrockInput([]agent.Message{{Role: "user", Content: "look", Images: []string{image}}}, nil, "model-1")
			if err == nil {
				t.Fatalf("bedrockInput accepted invalid image %q", image)
			}
		})
	}
}

func TestBedrockInputAcceptsGIFAndWEBPImages(t *testing.T) {
	for _, image := range []string{"data:image/gif;base64,AQ==", "data:image/webp;base64,Ag=="} {
		t.Run(image, func(t *testing.T) {
			input, err := bedrockInput([]agent.Message{{Role: "user", Content: "look", Images: []string{image}}}, nil, "model-1")
			if err != nil {
				t.Fatal(err)
			}
			block, ok := input.Messages[0].Content[1].(*bedrocktypes.ContentBlockMemberImage)
			if !ok {
				t.Fatalf("image block=%#v", input.Messages[0].Content)
			}
			if image == "data:image/gif;base64,AQ==" && block.Value.Format != bedrocktypes.ImageFormatGif {
				t.Fatalf("format=%q, want gif", block.Value.Format)
			}
			if image == "data:image/webp;base64,Ag==" && block.Value.Format != bedrocktypes.ImageFormatWebp {
				t.Fatalf("format=%q, want webp", block.Value.Format)
			}
		})
	}
}

func TestBedrockInputAcceptsJPGImageAlias(t *testing.T) {
	input, err := bedrockInput([]agent.Message{{Role: "user", Content: "look", Images: []string{"data:image/jpg;base64,AQ=="}}}, nil, "model-1")
	if err != nil {
		t.Fatal(err)
	}
	image, ok := input.Messages[0].Content[1].(*bedrocktypes.ContentBlockMemberImage)
	if !ok || image.Value.Format != bedrocktypes.ImageFormatJpeg {
		t.Fatalf("image=%#v", input.Messages[0].Content)
	}
}

func TestBedrockListsModelsFromAWSCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/foundation-models" {
			t.Fatalf("method=%s path=%q", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"modelSummaries":[{"modelId":"z-model"},{"modelId":"a-model"},{"modelId":""}]}`))
	}))
	defer server.Close()
	cfg := sdk.Config{
		Region:       "us-east-1",
		BaseEndpoint: sdk.String(server.URL),
		Credentials:  credentials.NewStaticCredentialsProvider("access", "secret", ""),
	}
	provider := NewBedrockConverse("us-east-1", "model")
	provider.CatalogClient = bedrock.NewFromConfig(cfg)
	models, err := provider.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "a-model" || models[1].ID != "z-model" || models[0].Provider != "amazon-bedrock" {
		t.Fatalf("models=%#v", models)
	}
}
