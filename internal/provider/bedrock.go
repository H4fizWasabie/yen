package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	bedrockdocument "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	bedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	smithybearer "github.com/aws/smithy-go/auth/bearer"

	"github.com/H4fizWasabie/yen/internal/agent"
)

// BedrockConverse implements the AWS credential-chain ConverseStream API.
type BedrockConverse struct {
	Region        string
	Profile       string
	BaseURL       string
	BearerToken   string
	SkipAuth      bool
	Model         string
	ProviderName  string
	Client        *bedrockruntime.Client
	CatalogClient *bedrock.Client
}

func NewBedrockConverse(region, model string) BedrockConverse {
	return BedrockConverse{Region: region, Model: model, ProviderName: "amazon-bedrock"}
}

func (p BedrockConverse) ListModels(ctx context.Context) ([]ModelInfo, error) {
	client := p.CatalogClient
	if client == nil {
		cfg, err := p.awsConfig(ctx)
		if err != nil {
			return nil, err
		}
		client = bedrock.NewFromConfig(cfg)
	}
	out, err := client.ListFoundationModels(ctx, &bedrock.ListFoundationModelsInput{})
	if err != nil {
		return nil, err
	}
	providerName := p.ProviderName
	if providerName == "" {
		providerName = "amazon-bedrock"
	}
	models := make([]ModelInfo, 0, len(out.ModelSummaries))
	for _, summary := range out.ModelSummaries {
		if summary.ModelId != nil && strings.TrimSpace(*summary.ModelId) != "" {
			models = append(models, ModelInfo{Provider: providerName, ID: *summary.ModelId})
		}
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
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
		cfg, err := p.awsConfig(ctx)
		if err != nil {
			return agent.Response{}, err
		}
		if p.BaseURL != "" {
			cfg.BaseEndpoint = aws.String(p.BaseURL)
		}
		client = bedrockruntime.NewFromConfig(cfg)
	}
	in, err := bedrockInput(ctx, messages, toolNames, p.Model)
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
	redactedReasoning := make(map[int][]byte)
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
				} else if signature, ok := delta.Value.(*bedrocktypes.ReasoningContentBlockDeltaMemberSignature); ok {
					result.ThinkingSignature += signature.Value
					partial.ThinkingSignature += signature.Value
				} else if redacted, ok := delta.Value.(*bedrocktypes.ReasoningContentBlockDeltaMemberRedactedContent); ok {
					redactedReasoning[index] = append(redactedReasoning[index], redacted.Value...)
					if !strings.Contains(result.Thinking, "[Reasoning redacted]") {
						result.Thinking += "[Reasoning redacted]"
						partial.Thinking += "[Reasoning redacted]"
					}
					if emit != nil {
						emit(agent.StreamEvent{Type: "thinking_delta", ContentIndex: index, Delta: "[Reasoning redacted]", Partial: partial})
					}
				}
			}
		case *bedrocktypes.ConverseStreamOutputMemberMessageStop:
			result.StopReason = bedrockStopReason(string(event.Value.StopReason))
		case *bedrocktypes.ConverseStreamOutputMemberMetadata:
			if usage := event.Value.Usage; usage != nil {
				applyBedrockUsage(&result, usage)
			}
		}
	}
	if err := out.GetStream().Err(); err != nil {
		return agent.Response{}, err
	}
	if len(redactedReasoning) > 0 {
		indices := make([]int, 0, len(redactedReasoning))
		for index := range redactedReasoning {
			indices = append(indices, index)
		}
		sort.Ints(indices)
		var opaque []byte
		for _, index := range indices {
			opaque = append(opaque, redactedReasoning[index]...)
		}
		result.ThinkingSignature = base64.StdEncoding.EncodeToString(opaque)
		partial.ThinkingSignature = result.ThinkingSignature
	}
	indices := make([]int, 0, len(toolCalls))
	for index := range toolCalls {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		call := toolCalls[index]
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

func applyBedrockUsage(result *agent.Response, usage *bedrocktypes.TokenUsage) {
	result.Usage.Input = int(aws.ToInt32(usage.InputTokens))
	result.Usage.Output = int(aws.ToInt32(usage.OutputTokens))
	result.Usage.CacheRead = int(aws.ToInt32(usage.CacheReadInputTokens))
	result.Usage.CacheWrite = int(aws.ToInt32(usage.CacheWriteInputTokens))
	result.Usage.TotalTokens = int(aws.ToInt32(usage.TotalTokens))
	if result.Usage.TotalTokens == 0 {
		result.Usage.TotalTokens = result.Usage.Input + result.Usage.Output + result.Usage.CacheRead + result.Usage.CacheWrite
	}
}

func (p BedrockConverse) awsConfig(ctx context.Context) (aws.Config, error) {
	region := p.Region
	if region == "" {
		region = os.Getenv("AWS_REGION")
	}
	if region == "" {
		region = os.Getenv("AWS_DEFAULT_REGION")
	}
	if region == "" {
		region = "us-east-1"
	}
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	if p.Profile != "" {
		options = append(options, awsconfig.WithSharedConfigProfile(p.Profile))
	}
	if token := p.bearerToken(); token != "" {
		options = append(options, awsconfig.WithBearerAuthTokenProvider(smithybearer.StaticTokenProvider{Token: smithybearer.Token{Value: token}}))
	}
	if p.SkipAuth {
		options = append(options, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("dummy-access-key", "dummy-secret-key", "")))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return aws.Config{}, err
	}
	if p.bearerToken() != "" && !p.SkipAuth {
		cfg.AuthSchemePreference = []string{"httpBearerAuth"}
	}
	return cfg, nil
}

func (p BedrockConverse) bearerToken() string {
	if p.BearerToken != "" {
		return p.BearerToken
	}
	if token := os.Getenv("YEN_AWS_BEARER_TOKEN_BEDROCK"); token != "" {
		return token
	}
	return os.Getenv("AWS_BEARER_TOKEN_BEDROCK")
}

func bedrockInput(ctx context.Context, messages []agent.Message, toolNames []string, model string) (*bedrockruntime.ConverseStreamInput, error) {
	return bedrockInputWithImageClient(ctx, messages, toolNames, model, nil)
}

func bedrockInputWithImageClient(ctx context.Context, messages []agent.Message, toolNames []string, model string, imageClient *http.Client) (*bedrockruntime.ConverseStreamInput, error) {
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
		content, err := bedrockMessageContent(ctx, message, model, imageClient)
		if err != nil {
			return nil, err
		}
		if len(content) == 0 {
			continue
		}
		input.Messages = append(input.Messages, bedrocktypes.Message{Role: bedrocktypes.ConversationRole(role), Content: content})
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

func bedrockMessageContent(ctx context.Context, message agent.Message, model string, imageClient *http.Client) ([]bedrocktypes.ContentBlock, error) {
	if message.ToolCallID != "" {
		content := []bedrocktypes.ToolResultContentBlock{&bedrocktypes.ToolResultContentBlockMemberText{Value: nonEmpty(message.Content)}}
		for _, image := range message.Images {
			block, err := bedrockImage(ctx, image, imageClient)
			if err != nil {
				return nil, err
			}
			content = append(content, &bedrocktypes.ToolResultContentBlockMemberImage{Value: block})
		}
		status := bedrocktypes.ToolResultStatusSuccess
		if message.ErrorMessage != "" {
			status = bedrocktypes.ToolResultStatusError
			content[0] = &bedrocktypes.ToolResultContentBlockMemberText{Value: nonEmpty(message.ErrorMessage)}
		}
		return []bedrocktypes.ContentBlock{&bedrocktypes.ContentBlockMemberToolResult{Value: bedrocktypes.ToolResultBlock{ToolUseId: aws.String(message.ToolCallID), Status: status, Content: content}}}, nil
	}
	content := make([]bedrocktypes.ContentBlock, 0, 1+len(message.Images)+len(message.ToolCalls))
	if message.Thinking != "" {
		if strings.HasPrefix(message.Thinking, "[Reasoning redacted]") {
			if opaque, err := base64.StdEncoding.DecodeString(message.ThinkingSignature); err == nil && len(opaque) > 0 {
				content = append(content, &bedrocktypes.ContentBlockMemberReasoningContent{Value: &bedrocktypes.ReasoningContentBlockMemberRedactedContent{Value: opaque}})
			}
		}
		if len(content) == 0 {
			thinking := bedrocktypes.ReasoningTextBlock{Text: aws.String(message.Thinking)}
			if message.ThinkingSignature != "" && strings.Contains(strings.ToLower(model), "claude") {
				thinking.Signature = aws.String(message.ThinkingSignature)
			}
			content = append(content, &bedrocktypes.ContentBlockMemberReasoningContent{Value: &bedrocktypes.ReasoningContentBlockMemberReasoningText{Value: thinking}})
		}
	}
	if strings.TrimSpace(message.Content) != "" {
		content = append(content, &bedrocktypes.ContentBlockMemberText{Value: message.Content})
	}
	for _, image := range message.Images {
		block, err := bedrockImage(ctx, image, imageClient)
		if err != nil {
			return nil, err
		}
		content = append(content, &bedrocktypes.ContentBlockMemberImage{Value: block})
	}
	for _, call := range message.ToolCalls {
		content = append(content, &bedrocktypes.ContentBlockMemberToolUse{Value: bedrocktypes.ToolUseBlock{
			ToolUseId: aws.String(call.ID), Name: aws.String(call.Name), Input: bedrockdocument.NewLazyDocument(call.Args),
		}})
	}
	return content, nil
}

func bedrockImage(ctx context.Context, value string, imageClient *http.Client) (bedrocktypes.ImageBlock, error) {
	mediaType, encoded, ok := parseDataImage(value)
	if !ok {
		imageURL, err := url.Parse(value)
		if err != nil || (imageURL.Scheme != "http" && imageURL.Scheme != "https") || imageURL.Host == "" {
			return bedrocktypes.ImageBlock{}, errors.New("invalid Bedrock image: expected a data URL or HTTP(S) URL")
		}
		if imageClient == nil {
			if err := validateBedrockImageURL(ctx, imageURL); err != nil {
				return bedrocktypes.ImageBlock{}, err
			}
			imageClient = newBedrockImageHTTPClient()
		} else {
			imageClient = withBedrockImageRedirectValidation(imageClient)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL.String(), nil)
		if err != nil {
			return bedrocktypes.ImageBlock{}, fmt.Errorf("prepare Bedrock image fetch: %w", err)
		}
		response, err := imageClient.Do(request)
		if err != nil {
			return bedrocktypes.ImageBlock{}, fmt.Errorf("fetch Bedrock image: %w", err)
		}
		defer response.Body.Close()
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			return bedrocktypes.ImageBlock{}, fmt.Errorf("fetch Bedrock image: HTTP %s", response.Status)
		}
		if response.ContentLength > maxBedrockImageBytes {
			return bedrocktypes.ImageBlock{}, errors.New("Bedrock image exceeds 10 MiB")
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, maxBedrockImageBytes+1))
		if err != nil {
			return bedrocktypes.ImageBlock{}, fmt.Errorf("read Bedrock image: %w", err)
		}
		if int64(len(data)) > maxBedrockImageBytes {
			return bedrocktypes.ImageBlock{}, errors.New("Bedrock image exceeds 10 MiB")
		}
		mimeType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
		if err != nil || !strings.HasPrefix(mimeType, "image/") {
			return bedrocktypes.ImageBlock{}, errors.New("invalid Bedrock image: HTTP(S) source must have an image content type")
		}
		mediaType = mimeType
		encoded = base64.StdEncoding.EncodeToString(data)
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return bedrocktypes.ImageBlock{}, fmt.Errorf("invalid Bedrock image data: %w", err)
	}
	format := bedrocktypes.ImageFormat(strings.TrimPrefix(mediaType, "image/"))
	if format == bedrocktypes.ImageFormat("jpg") {
		format = bedrocktypes.ImageFormatJpeg
	}
	if format != bedrocktypes.ImageFormatPng && format != bedrocktypes.ImageFormatJpeg && format != bedrocktypes.ImageFormatGif && format != bedrocktypes.ImageFormatWebp {
		return bedrocktypes.ImageBlock{}, fmt.Errorf("unsupported Bedrock image type %q", mediaType)
	}
	return bedrocktypes.ImageBlock{Format: format, Source: &bedrocktypes.ImageSourceMemberBytes{Value: data}}, nil
}

const maxBedrockImageBytes = 10 << 20

func withBedrockImageRedirectValidation(client *http.Client) *http.Client {
	clone := *client
	previous := client.CheckRedirect
	clone.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if err := validateBedrockImageURL(request.Context(), request.URL); err != nil {
			return err
		}
		if previous != nil {
			return previous(request, via)
		}
		return nil
	}
	return &clone
}

func newBedrockImageHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				return dialBedrockImage(ctx, network, address)
			},
		},
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			return validateBedrockImageURL(request.Context(), request.URL)
		},
	}
}

func validateBedrockImageURL(ctx context.Context, imageURL *url.URL) error {
	if imageURL == nil || imageURL.Hostname() == "" {
		return errors.New("invalid Bedrock image URL host")
	}
	if _, err := safeBedrockImageIPs(ctx, imageURL.Hostname()); err != nil {
		return fmt.Errorf("unsafe Bedrock image URL: %w", err)
	}
	return nil
}

func dialBedrockImage(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid Bedrock image address: %w", err)
	}
	ips, err := safeBedrockImageIPs(ctx, host)
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{}
	var lastErr error
	for _, ip := range ips {
		connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return connection, nil
		}
		lastErr = dialErr
	}
	return nil, lastErr
}

func safeBedrockImageIPs(ctx context.Context, host string) ([]net.IP, error) {
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("resolve %q: no addresses", host)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() || ip.IsMulticast() {
			return nil, fmt.Errorf("address %s is private or reserved", ip)
		}
	}
	return ips, nil
}

func nonEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return "<empty>"
	}
	return value
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
