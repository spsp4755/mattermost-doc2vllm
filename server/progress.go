package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mattermost/mattermost/server/public/model"
)

const (
	mattermostSafePostMessageRunes = model.PostMessageMaxRunesV2 - 256
	mattermostSafePostPropsRunes   = 64 * 1024
	mattermostDebugPropRunes       = 12 * 1024
	mattermostRetryDebugPropRunes  = 4 * 1024
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

	message, props = sanitizeBotPostPayload(message, props)
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
		p.API.LogWarn("Failed to update Doc2VLLM post; creating a new post instead", "post_id", existing.Id, "error", appErr.Error(), "message_runes", utf8.RuneCountInString(message), "props_runes", countPostPropsRunes(props))
		return p.createBotPost(channel, rootID, account, message, props)
	}
	return post, nil
}

func (p *Plugin) createBotPost(channel *model.Channel, rootID string, account botAccount, message string, props map[string]any) (*model.Post, error) {
	message, props = sanitizeBotPostPayload(message, props)

	post, appErr := p.API.CreatePost(&model.Post{
		UserId:    account.UserID,
		ChannelId: channel.Id,
		RootId:    rootID,
		Type:      doc2vllmBotPostType,
		Message:   message,
		Props:     props,
	})
	if appErr != nil {
		p.API.LogWarn("Failed to create Doc2VLLM post; retrying with reduced payload", "channel_id", channel.Id, "root_id", rootID, "bot_user_id", account.UserID, "error", appErr.Error(), "message_runes", utf8.RuneCountInString(message), "props_runes", countPostPropsRunes(props))

		retryMessage := truncateString(message, mattermostSafePostMessageRunes/2)
		retryProps := minimizeBotPostPropsForRetry(props)
		post, retryErr := p.API.CreatePost(&model.Post{
			UserId:    account.UserID,
			ChannelId: channel.Id,
			RootId:    rootID,
			Type:      doc2vllmBotPostType,
			Message:   retryMessage,
			Props:     retryProps,
		})
		if retryErr != nil {
			return nil, fmt.Errorf("failed to create Doc2VLLM post: %w (retry failed: %s)", appErr, retryErr.Error())
		}
		return post, nil
	}
	return post, nil
}

func sanitizeBotPostPayload(message string, props map[string]any) (string, map[string]any) {
	safeMessage := truncateString(strings.ToValidUTF8(strings.TrimSpace(message), ""), mattermostSafePostMessageRunes)
	safeProps := copyBotPostProps(props)
	if len(safeProps) == 0 {
		return safeMessage, nil
	}

	for key, value := range safeProps {
		text, ok := value.(string)
		if !ok {
			continue
		}
		limit := mattermostSafePostMessageRunes
		if isDoc2VLLMDebugPropKey(key) {
			limit = mattermostDebugPropRunes
		}
		safeProps[key] = truncateString(strings.ToValidUTF8(text, ""), limit)
	}

	if countPostPropsRunes(safeProps) > mattermostSafePostPropsRunes {
		for _, key := range []string{"doc2vllm_response_output", "doc2vllm_error_output"} {
			delete(safeProps, key)
		}
	}
	if countPostPropsRunes(safeProps) > mattermostSafePostPropsRunes {
		for _, key := range []string{"doc2vllm_request_input", "doc2vllm_error_input"} {
			if value, ok := safeProps[key].(string); ok {
				safeProps[key] = truncateString(value, mattermostRetryDebugPropRunes)
			}
		}
	}
	if countPostPropsRunes(safeProps) > mattermostSafePostPropsRunes {
		for _, key := range []string{"doc2vllm_request_input", "doc2vllm_error_input"} {
			delete(safeProps, key)
		}
	}
	if countPostPropsRunes(safeProps) > model.PostPropsMaxUserRunes {
		safeProps = minimizeBotPostPropsForRetry(safeProps)
	}

	return safeMessage, safeProps
}

func minimizeBotPostPropsForRetry(props map[string]any) map[string]any {
	if len(props) == 0 {
		return nil
	}

	reduced := map[string]any{}
	for _, key := range []string{
		"from_bot",
		"doc2vllm_bot_id",
		"doc2vllm_correlation_id",
		"doc2vllm_api_duration_ms",
		"doc2vllm_model",
		"doc2vllm_ocr",
		"doc2vllm_error",
		"doc2vllm_error_code",
		"doc2vllm_progress",
		"doc2vllm_stage",
	} {
		value, ok := props[key]
		if !ok {
			continue
		}
		if text, ok := value.(string); ok {
			reduced[key] = truncateString(strings.ToValidUTF8(text, ""), 256)
			continue
		}
		reduced[key] = value
	}
	if len(reduced) == 0 {
		return nil
	}
	return reduced
}

func copyBotPostProps(props map[string]any) map[string]any {
	if len(props) == 0 {
		return nil
	}
	copied := make(map[string]any, len(props))
	for key, value := range props {
		copied[key] = value
	}
	return copied
}

func countPostPropsRunes(props map[string]any) int {
	if len(props) == 0 {
		return 0
	}
	return utf8.RuneCountInString(model.StringInterfaceToJSON(props))
}

func isDoc2VLLMDebugPropKey(key string) bool {
	switch strings.TrimSpace(key) {
	case "doc2vllm_request_input", "doc2vllm_response_output", "doc2vllm_error_input", "doc2vllm_error_output":
		return true
	default:
		return false
	}
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
