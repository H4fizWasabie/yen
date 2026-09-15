package codingagent

import "github.com/H4fizWasabie/yen/internal/agent"

const workingNotePromptPrefix = "<working_note>\nEstablished by earlier turns; verify this note if it contradicts current evidence.\n"

func WorkingNoteMessage(note string) agent.Message {
	runes := []rune(note)
	if len(runes) > 2000 {
		runes = append(append([]rune(nil), runes[:1000]...), append([]rune("\n...\n"), runes[len(runes)-1000:]...)...)
	}
	return agent.Message{
		Role:    "system",
		Content: workingNotePromptPrefix + string(runes) + "\n</working_note>",
	}
}
