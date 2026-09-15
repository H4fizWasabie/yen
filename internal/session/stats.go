package session

// Stats returns the compatibility-shaped session counters used by headless
// and terminal clients.
func Stats(current *Session) map[string]any {
	stats := map[string]any{
		"sessionFile": current.Path(), "sessionId": current.Header().ID,
		"userMessages": 0, "assistantMessages": 0, "toolCalls": 0, "toolResults": 0,
		"totalMessages": 0,
		"tokens":        map[string]int{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0, "total": 0},
	}
	for _, message := range current.Messages() {
		stats["totalMessages"] = stats["totalMessages"].(int) + 1
		switch message.Role {
		case "user":
			stats["userMessages"] = stats["userMessages"].(int) + 1
		case "assistant":
			stats["assistantMessages"] = stats["assistantMessages"].(int) + 1
		case "toolResult":
			stats["toolResults"] = stats["toolResults"].(int) + 1
		}
		if message.Usage != nil {
			tokens := stats["tokens"].(map[string]int)
			tokens["input"] += message.Usage.Input
			tokens["output"] += message.Usage.Output
			tokens["cacheRead"] += message.Usage.CacheRead
			tokens["cacheWrite"] += message.Usage.CacheWrite
			tokens["total"] += message.Usage.TotalTokens
		}
		if parts, ok := message.Content.([]ContentPart); ok {
			for _, part := range parts {
				if part.Type == "toolCall" {
					stats["toolCalls"] = stats["toolCalls"].(int) + 1
				}
			}
		}
	}
	return stats
}
