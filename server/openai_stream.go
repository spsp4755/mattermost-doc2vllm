package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func consumeOpenAITextStream(body io.Reader, onSnapshot func(string) error) (string, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	var builder strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			break
		}

		delta, err := extractStreamingDeltaText([]byte(payload))
		if err != nil {
			return strings.TrimSpace(builder.String()), err
		}
		if delta == "" {
			continue
		}

		builder.WriteString(delta)
		if onSnapshot != nil {
			if err := onSnapshot(strings.TrimSpace(builder.String())); err != nil {
				return strings.TrimSpace(builder.String()), err
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return strings.TrimSpace(builder.String()), err
	}
	return strings.TrimSpace(builder.String()), nil
}

func extractStreamingDeltaText(raw []byte) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", nil
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("failed to decode streaming payload: %w", err)
	}

	choices, ok := payload["choices"].([]any)
	if !ok {
		return extractTextFromValue(payload), nil
	}

	parts := make([]string, 0, len(choices))
	for _, choice := range choices {
		item, ok := choice.(map[string]any)
		if !ok {
			continue
		}
		if text := extractStreamChoiceText(item); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, ""), nil
}

func extractStreamChoiceText(choice map[string]any) string {
	for _, key := range []string{"delta", "message"} {
		if raw, ok := choice[key]; ok {
			if text := extractStreamingContentText(raw); text != "" {
				return text
			}
		}
	}
	if raw, ok := choice["text"]; ok {
		if text := extractStreamingContentText(raw); text != "" {
			return text
		}
	}
	return ""
}

func extractStreamingContentText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		if content, ok := typed["content"]; ok {
			return extractStreamingContentText(content)
		}
		if text, ok := typed["text"]; ok {
			return extractStreamingContentText(text)
		}
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := extractStreamingContentText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	}
	return extractTextFromValue(value)
}
