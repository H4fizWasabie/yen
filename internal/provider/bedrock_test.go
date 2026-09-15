package provider

import (
	"testing"

	"github.com/H4fizWasabie/yen/internal/agent"
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
