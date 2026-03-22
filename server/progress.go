package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

type botProgressPost struct {
	plugin        *Plugin
	channel       *model.Channel
	rootID        string
	account       botAccount
	correlationID string
	post          *model.Post
	updateEvery   time.Duration
	lastMessage   string
	lastUpdate    time.Time
	mu            sync.Mutex
}

func (p *Plugin) newBotProgressPost(channel *model.Channel, rootID string, account botAccount, correlationID string, cfg *runtimeConfiguration, startedAt time.Time) (*botProgressPost, error) {
	if channel == nil || account.UserID == "" {
		return nil, nil
	}

	updateEvery := time.Duration(defaultStreamingUpdateMS) * time.Millisecond
	if cfg != nil && cfg.StreamingUpdateMS > 0 {
		updateEvery = time.Duration(cfg.StreamingUpdateMS) * time.Millisecond
	}

	progress := &botProgressPost{
		plugin:        p,
		channel:       channel,
		rootID:        rootID,
		account:       account,
		correlationID: correlationID,
		updateEvery:   updateEvery,
	}

	initialMessage := buildBotProgressMessage("요청 접수", "첨부 파일과 입력 내용을 확인하고 있습니다.", "", correlationID, time.Since(startedAt))
	post, err := p.upsertBotPost(channel, rootID, account, nil, initialMessage, map[string]any{
		"from_bot":                "true",
		"doc2vllm_bot_id":         account.Definition.ID,
		"doc2vllm_correlation_id": correlationID,
		"doc2vllm_model":          account.Definition.Model,
		"doc2vllm_progress":       "true",
		"doc2vllm_ocr":            "true",
		"doc2vllm_stage":          "queued",
	})
	if err != nil {
		return nil, err
	}

	progress.post = post
	progress.lastMessage = initialMessage
	progress.lastUpdate = time.Now()
	return progress, nil
}

func (p *Plugin) upsertBotPost(channel *model.Channel, rootID string, account botAccount, existing *model.Post, message string, props map[string]any) (*model.Post, error) {
	if err := p.ensureBotInChannel(channel.Id, account.UserID); err != nil {
		return nil, err
	}

	message = strings.TrimSpace(message)
	if message == "" {
		message = "_빈 응답이 반환되었습니다._"
	}

	if existing == nil || existing.Id == "" {
		post, appErr := p.API.CreatePost(&model.Post{
			UserId:    account.UserID,
			ChannelId: channel.Id,
			RootId:    rootID,
			Type:      doc2vllmBotPostType,
			Message:   message,
			Props:     props,
		})
		if appErr != nil {
			return nil, fmt.Errorf("failed to create Doc2VLLM post: %w", appErr)
		}
		return post, nil
	}

	post, appErr := p.API.UpdatePost(&model.Post{
		Id:        existing.Id,
		UserId:    account.UserID,
		ChannelId: channel.Id,
		RootId:    rootID,
		Type:      doc2vllmBotPostType,
		Message:   message,
		Props:     props,
	})
	if appErr != nil {
		return nil, fmt.Errorf("failed to update Doc2VLLM post: %w", appErr)
	}
	return post, nil
}

func (p *Plugin) updateProgressPost(progress *botProgressPost, stage, detail, partial string, startedAt time.Time, force bool) error {
	if progress == nil {
		return nil
	}

	progress.mu.Lock()
	defer progress.mu.Unlock()

	if progress.post == nil {
		return nil
	}

	message := buildBotProgressMessage(stage, detail, partial, progress.correlationID, time.Since(startedAt))
	if !force {
		if message == progress.lastMessage {
			return nil
		}
		if progress.updateEvery > 0 && time.Since(progress.lastUpdate) < progress.updateEvery {
			return nil
		}
	}

	post, err := p.upsertBotPost(progress.channel, progress.rootID, progress.account, progress.post, message, map[string]any{
		"from_bot":                "true",
		"doc2vllm_bot_id":         progress.account.Definition.ID,
		"doc2vllm_correlation_id": progress.correlationID,
		"doc2vllm_model":          progress.account.Definition.Model,
		"doc2vllm_progress":       "true",
		"doc2vllm_ocr":            "true",
		"doc2vllm_stage":          strings.TrimSpace(stage),
	})
	if err != nil {
		return err
	}

	progress.post = post
	progress.lastMessage = message
	progress.lastUpdate = time.Now()
	return nil
}

func buildBotProgressMessage(stage, detail, partial, correlationID string, elapsed time.Duration) string {
	stage = strings.TrimSpace(stage)
	detail = strings.TrimSpace(detail)
	partial = strings.TrimSpace(partial)
	partial = truncateString(partial, 6000)

	if detail == "" {
		detail = "요청을 처리하고 있습니다."
	}
	if stage == "" {
		stage = "처리 중"
	}

	lines := make([]string, 0, 8)
	if partial != "" {
		lines = append(lines, partial, "", "---")
	}
	lines = append(lines,
		fmt.Sprintf("_%s_", detail),
		"",
		fmt.Sprintf("- 상태: `%s`", stage),
		fmt.Sprintf("- 경과 시간: `%s`", formatDoc2VLLMAPIDuration(elapsed)),
		fmt.Sprintf("- Correlation ID: `%s`", correlationID),
	)
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
