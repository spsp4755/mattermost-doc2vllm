package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const threadConversationKeyPrefix = "thread_conversation_"

type threadConversationState struct {
	BotID           string             `json:"bot_id"`
	ChannelID       string             `json:"channel_id"`
	RootID          string             `json:"root_id"`
	DocumentContext string             `json:"document_context"`
	Turns           []conversationTurn `json:"turns"`
	UpdatedAt       int64              `json:"updated_at"`
}

type conversationTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (p *Plugin) getThreadConversationState(rootID string) (*threadConversationState, error) {
	rootID = strings.TrimSpace(rootID)
	if rootID == "" {
		return nil, nil
	}

	data, appErr := p.API.KVGet(threadConversationKeyPrefix + rootID)
	if appErr != nil {
		return nil, fmt.Errorf("failed to load thread conversation state: %w", appErr)
	}
	if len(data) == 0 {
		return nil, nil
	}

	var state threadConversationState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to decode thread conversation state: %w", err)
	}
	return &state, nil
}

func (p *Plugin) saveThreadConversationState(state threadConversationState) error {
	state.RootID = strings.TrimSpace(state.RootID)
	if state.RootID == "" {
		return nil
	}
	state.BotID = strings.TrimSpace(state.BotID)
	state.ChannelID = strings.TrimSpace(state.ChannelID)
	state.DocumentContext = strings.TrimSpace(state.DocumentContext)
	state.Turns = normalizeConversationTurns(state.Turns)
	state.UpdatedAt = time.Now().UnixMilli()

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to encode thread conversation state: %w", err)
	}
	if appErr := p.API.KVSet(threadConversationKeyPrefix+state.RootID, data); appErr != nil {
		return fmt.Errorf("failed to persist thread conversation state: %w", appErr)
	}
	return nil
}

func normalizeConversationTurns(turns []conversationTurn) []conversationTurn {
	normalized := make([]conversationTurn, 0, len(turns))
	for _, turn := range turns {
		role := strings.ToLower(strings.TrimSpace(turn.Role))
		content := strings.TrimSpace(turn.Content)
		if role == "" || content == "" {
			continue
		}
		normalized = append(normalized, conversationTurn{
			Role:    role,
			Content: content,
		})
	}
	if len(normalized) > 12 {
		normalized = normalized[len(normalized)-12:]
	}
	return normalized
}

func buildConversationDocumentContext(prompt string, results []doc2vllmDocumentResult, failures []documentProcessingFailure, maxLength int) string {
	sections := make([]string, 0, len(results))
	for _, result := range results {
		_, content := buildRenderableDoc2VLLMContent(result.Response)
		lines := []string{
			fmt.Sprintf("[Document] %s", result.Attachment.Name),
		}
		if strings.TrimSpace(result.RequestPrompt) != "" {
			lines = append(lines, fmt.Sprintf("Prompt: %s", result.RequestPrompt))
		}
		if strings.TrimSpace(result.Source) != "" {
			lines = append(lines, fmt.Sprintf("Source: %s", result.Source))
		}
		if strings.TrimSpace(result.Processor) != "" {
			lines = append(lines, fmt.Sprintf("Processor: %s", result.Processor))
		}
		if strings.TrimSpace(content) != "" {
			lines = append(lines, "", strings.TrimSpace(content))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}

	if len(failures) > 0 {
		sections = append(sections, "[Processing failures]\n"+summarizeDocumentFailureMessages(failures, 5))
	}
	if strings.TrimSpace(prompt) != "" {
		sections = append([]string{"[Initial user request]\n" + strings.TrimSpace(prompt)}, sections...)
	}

	return truncateString(strings.TrimSpace(strings.Join(sections, "\n\n")), maxLength)
}
