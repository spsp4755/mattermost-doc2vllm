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

	initialMessage := buildBotProgressMessage(
		"\uc694\uccad \uc811\uc218",
		"\ucca8\ubd80 \ud30c\uc77c\uacfc \uc785\ub825 \ub0b4\uc6a9\uc744 \ud655\uc778\ud558\uace0 \uc788\uc2b5\ub2c8\ub2e4.",
		"",
		correlationID,
		time.Since(startedAt),
	)
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
		message = "_\ube48 \uc751\ub2f5\uc774 \ubc18\ud658\ub418\uc5c8\uc2b5\ub2c8\ub2e4._"
	}

	if existing == nil || existing.Id == "" {
		return p.createBotPost(channel, rootID, account, message, props)
	}

	postForUpdate, appErr := p.API.GetPost(existing.Id)
	if appErr != nil {
		p.API.LogWarn("Failed to load existing Doc2VLLM post before update; creating a new post instead", "post_id", existing.Id, "error", appErr.Error())
		return p.createBotPost(channel, rootID, account, message, props)
	}

	postForUpdate.UserId = account.UserID
	postForUpdate.ChannelId = channel.Id
	postForUpdate.RootId = rootID
	postForUpdate.Type = doc2vllmBotPostType
	postForUpdate.Message = message
	postForUpdate.Props = props

	post, appErr := p.API.UpdatePost(postForUpdate)
	if appErr != nil {
		p.API.LogWarn("Failed to update Doc2VLLM post; creating a new post instead", "post_id", existing.Id, "error", appErr.Error())
		return p.createBotPost(channel, rootID, account, message, props)
	}
	return post, nil
}

func (p *Plugin) createBotPost(channel *model.Channel, rootID string, account botAccount, message string, props map[string]any) (*model.Post, error) {
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
		detail = "\uc694\uccad\uc744 \ucc98\ub9ac\ud558\uace0 \uc788\uc2b5\ub2c8\ub2e4."
	}
	if stage == "" {
		stage = "\ucc98\ub9ac \uc911"
	}

	lines := make([]string, 0, 8)
	if partial != "" {
		lines = append(lines, partial, "", "---")
	}
	lines = append(lines,
		fmt.Sprintf("_%s_", detail),
		"",
		fmt.Sprintf("- \uc0c1\ud0dc: `%s`", stage),
		fmt.Sprintf("- \uacbd\uacfc \uc2dc\uac04: `%s`", formatDoc2VLLMAPIDuration(elapsed)),
		fmt.Sprintf("- Correlation ID: `%s`", correlationID),
	)
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
