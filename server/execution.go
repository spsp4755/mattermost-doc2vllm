package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/pluginapi"
)

type BotRunRequest struct {
	BotID         string         `json:"bot_id"`
	UserID        string         `json:"user_id"`
	UserName      string         `json:"user_name"`
	ChannelID     string         `json:"channel_id"`
	RootID        string         `json:"root_id"`
	Prompt        string         `json:"prompt"`
	Inputs        map[string]any `json:"inputs"`
	FileIDs       []string       `json:"file_ids,omitempty"`
	Source        string         `json:"source"`
	TriggerPostID string         `json:"trigger_post_id"`
}

type BotRunResult struct {
	CorrelationID string `json:"correlation_id"`
	BotID         string `json:"bot_id"`
	BotUsername   string `json:"bot_username"`
	BotName       string `json:"bot_name"`
	Model         string `json:"model"`
	APIDurationMS int64  `json:"api_duration_ms,omitempty"`
	PostID        string `json:"post_id,omitempty"`
	Status        string `json:"status"`
	Output        string `json:"output,omitempty"`
	ErrorMessage  string `json:"error_message,omitempty"`
	ErrorCode     string `json:"error_code,omitempty"`
	ErrorDetail   string `json:"error_detail,omitempty"`
	ErrorHint     string `json:"error_hint,omitempty"`
	RequestURL    string `json:"request_url,omitempty"`
	HTTPStatus    int    `json:"http_status,omitempty"`
	Retryable     bool   `json:"retryable"`
}

type executionFailureView struct {
	HasFailure  bool
	StageLabel  string
	Message     string
	ErrorCode   string
	Detail      string
	Hint        string
	RequestURL  string
	HTTPStatus  int
	Retryable   bool
	InputDebug  string
	OutputDebug string
	APIDuration time.Duration
}

type successDebugView struct {
	Request string
	Output  string
}

const doc2vllmBotPostType = "custom_doc2vllm_bot"

func (p *Plugin) executeBotAndPost(ctx context.Context, request BotRunRequest) (*BotRunResult, error) {
	startedAt := time.Now()
	correlationID := uuid.NewString()

	cfg, err := p.getRuntimeConfiguration()
	if err != nil {
		return nil, err
	}

	channel, appErr := p.API.GetChannel(request.ChannelID)
	if appErr != nil {
		return nil, fmt.Errorf("failed to load channel: %w", appErr)
	}
	user, appErr := p.API.GetUser(request.UserID)
	if appErr != nil {
		return nil, fmt.Errorf("failed to load user: %w", appErr)
	}
	request.UserName = user.Username
	team := p.getTeamForChannel(channel)

	bot := cfg.getBotByID(request.BotID)
	if bot == nil {
		return nil, fmt.Errorf("unknown bot %q", request.BotID)
	}
	if !bot.isAllowedFor(user, channel, team) {
		return nil, fmt.Errorf("bot %q is not allowed in this context", bot.Username)
	}
	if !p.client.User.HasPermissionToChannel(request.UserID, request.ChannelID, model.PermissionReadChannel) {
		return nil, fmt.Errorf("user does not have access to the selected channel")
	}

	account, ok := p.getBotAccount(bot.ID)
	if !ok {
		if err := p.ensureBots(); err != nil {
			return nil, err
		}
		account, ok = p.getBotAccount(bot.ID)
		if !ok {
			return nil, fmt.Errorf("bot account %q is not available", bot.ID)
		}
	}

	prompt := strings.TrimSpace(request.Prompt)
	if len(prompt) > cfg.MaxInputLength {
		return nil, fmt.Errorf("message exceeds the maximum input length of %d characters", cfg.MaxInputLength)
	}

	attachments, err := p.collectBotAttachments(request.FileIDs, request.ChannelID)
	if err != nil {
		return nil, err
	}
	if len(attachments) == 0 && strings.TrimSpace(request.RootID) != "" {
		return p.executeThreadConversation(ctx, cfg, request, *bot, account, channel, startedAt, correlationID)
	}
	preparedInputs, processingFailures := p.prepareOCRInputs(ctx, cfg, attachments)
	if len(preparedInputs) == 0 && len(processingFailures) == 0 {
		return nil, fmt.Errorf("attach at least one image, PDF, DOCX, XLSX, or PPTX file before asking @%s", bot.Username)
	}

	serviceConfig, err := cfg.serviceConfigForBot(*bot)
	if err != nil {
		return nil, err
	}

	results := make([]doc2vllmDocumentResult, 0, len(preparedInputs))
	requestDebugs := make([]doc2vllmRequestDebug, 0, len(preparedInputs))
	apiDurationTotal := time.Duration(0)
	for _, preparedInput := range preparedInputs {
		if preparedInput.DirectResult != nil {
			results = append(results, *preparedInput.DirectResult)
			continue
		}

		attachment := preparedInput.Attachment
		result, _, apiDuration, invokeErr := p.invokeDoc2VLLMOCR(ctx, serviceConfig, *bot, attachment, prompt, correlationID)
		apiDurationTotal += apiDuration
		if invokeErr != nil {
			invokeErr = attachDoc2VLLMAttemptDebug(invokeErr, requestDebugs)
			processingFailures = append(processingFailures, newDocumentProcessingFailure(attachment.Name, invokeErr))
			continue
		}
		results = append(results, result)
		requestDebugs = append(requestDebugs, result.RequestDebugs...)
	}

	if len(results) == 0 {
		failure := buildExecutionFailureFromDocumentFailures(processingFailures, apiDurationTotal)
		record := newExecutionRecord(request, account.Definition, correlationID, "failed", prompt, failure.Message, failure.ErrorCode, failure.Retryable, startedAt, time.Now())
		p.appendExecutionHistory(request.UserID, record)
		p.logUsage(cfg, correlationID, request, account.Definition, "failed", failure.Message)
		if postErr := p.postFailure(channel, request.RootID, account, correlationID, failure); postErr != nil {
			p.API.LogError("Failed to post Doc2VLLM error response", "error", postErr, "correlation_id", correlationID)
		}
		return &BotRunResult{
			CorrelationID: correlationID,
			BotID:         account.Definition.ID,
			BotUsername:   account.Definition.Username,
			BotName:       account.Definition.DisplayName,
			Model:         account.Definition.Model,
			APIDurationMS: apiDurationTotal.Milliseconds(),
			Status:        "failed",
			ErrorMessage:  failure.Message,
			ErrorCode:     failure.ErrorCode,
			ErrorDetail:   failure.Detail,
			ErrorHint:     failure.Hint,
			RequestURL:    failure.RequestURL,
			HTTPStatus:    failure.HTTPStatus,
			Retryable:     failure.Retryable,
		}, errors.New(failure.Message)
	}

	shouldMaskSensitive := bot.shouldMaskSensitiveData(cfg.MaskSensitiveData)
	documentContext := buildDocumentResponseMarkdown(prompt, results, processingFailures, cfg.MaxOutputLength)
	if shouldMaskSensitive {
		documentContext = truncateString(maskSensitiveContent(documentContext), cfg.MaxOutputLength)
	}

	output := buildDocumentResponseOutput(bot.effectiveOutputMode(), prompt, results, processingFailures, cfg.MaxOutputLength)
	if shouldMaskSensitive {
		output = truncateString(maskSensitiveContent(output), cfg.MaxOutputLength)
	}
	debugView := successDebugView{
		Request: buildSuccessRequestDebugPayload(requestDebugs, ""),
	}
	if bot.hasVLLMPostProcess() {
		vllmConfig, vllmErr := cfg.serviceConfigForVLLMBot(*bot)
		if vllmErr != nil {
			output = buildVLLMFallbackOutput(documentContext, "vLLM 후처리 설정을 확인하지 못해 Doc2VLLM OCR 결과를 대신 표시합니다.")
		}
		if vllmErr == nil {
			vllmOutput, vllmDebug, invokeErr := p.invokeVLLMPostProcess(ctx, vllmConfig, prompt, documentContext, correlationID)
			if invokeErr != nil {
				failure := describeExecutionFailure(invokeErr, true, apiDurationTotal)
				output = buildVLLMFallbackOutput(output, "vLLM 후처리에 실패해 Doc2VLLM OCR 결과를 대신 표시합니다.")
				debugView = successDebugView{
					Request: buildSuccessRequestDebugPayload(requestDebugs, failure.InputDebug),
					Output:  failure.OutputDebug,
				}
				p.API.LogWarn("vLLM post-processing failed; falling back to Doc2VLLM OCR output", "correlation_id", correlationID, "bot_id", bot.ID, "error", failure.Message)
			} else {
				output = truncateString(vllmOutput, cfg.MaxOutputLength)
				debugView = successDebugView{
					Request: buildSuccessRequestDebugPayload(requestDebugs, vllmDebug),
				}
			}
		}
	}

	post, err := p.postSuccess(channel, request.RootID, account, correlationID, output, debugView, apiDurationTotal)
	if err != nil {
		failure := describeExecutionFailure(err, true, apiDurationTotal)
		record := newExecutionRecord(request, account.Definition, correlationID, "failed", prompt, failure.Message, failure.ErrorCode, failure.Retryable, startedAt, time.Now())
		p.appendExecutionHistory(request.UserID, record)
		return nil, err
	}

	if request.RootID != "" {
		state := threadConversationState{
			BotID:           bot.ID,
			ChannelID:       request.ChannelID,
			RootID:          request.RootID,
			DocumentContext: buildConversationDocumentContext(prompt, results, processingFailures, cfg.MaxOutputLength*2),
			Turns: []conversationTurn{
				{Role: "user", Content: prompt},
				{Role: "assistant", Content: output},
			},
		}
		if saveErr := p.saveThreadConversationState(state); saveErr != nil {
			p.API.LogWarn("Failed to persist Doc2VLLM thread context", "error", saveErr, "root_id", request.RootID, "correlation_id", correlationID)
		}
	}

	record := newExecutionRecord(request, account.Definition, correlationID, "completed", prompt, "", "", false, startedAt, time.Now())
	status := "completed"
	if len(processingFailures) > 0 {
		status = "completed_partial"
	}
	record = newExecutionRecord(request, account.Definition, correlationID, status, prompt, summarizeDocumentFailureMessages(processingFailures, 2), "", false, startedAt, time.Now())
	p.appendExecutionHistory(request.UserID, record)
	p.logUsage(cfg, correlationID, request, account.Definition, status, summarizeDocumentFailureMessages(processingFailures, 2))

	return &BotRunResult{
		CorrelationID: correlationID,
		BotID:         account.Definition.ID,
		BotUsername:   account.Definition.Username,
		BotName:       account.Definition.DisplayName,
		Model:         account.Definition.Model,
		APIDurationMS: apiDurationTotal.Milliseconds(),
		PostID:        post.Id,
		Status:        status,
		Output:        output,
	}, nil
}

func (p *Plugin) executeThreadConversation(
	ctx context.Context,
	cfg *runtimeConfiguration,
	request BotRunRequest,
	bot BotDefinition,
	account botAccount,
	channel *model.Channel,
	startedAt time.Time,
	correlationID string,
) (*BotRunResult, error) {
	state, err := p.getThreadConversationState(request.RootID)
	if err != nil {
		return nil, err
	}
	if state == nil || strings.TrimSpace(state.DocumentContext) == "" {
		return nil, fmt.Errorf("attach at least one image, PDF, DOCX, XLSX, or PPTX file before asking @%s", bot.Username)
	}
	if state.BotID != "" && !strings.EqualFold(state.BotID, bot.ID) {
		return nil, fmt.Errorf("thread conversation is already bound to a different bot")
	}

	serviceConfig, err := cfg.serviceConfigForBot(bot)
	if err != nil {
		return nil, err
	}

	response, requestDebug, apiDuration, _, invokeErr := p.invokeDoc2VLLMConversation(
		ctx,
		serviceConfig,
		bot,
		state.DocumentContext,
		state.Turns,
		request.Prompt,
		correlationID,
	)
	if invokeErr != nil {
		failure := describeExecutionFailure(invokeErr, true, apiDuration)
		record := newExecutionRecord(request, account.Definition, correlationID, "failed", request.Prompt, failure.Message, failure.ErrorCode, failure.Retryable, startedAt, time.Now())
		p.appendExecutionHistory(request.UserID, record)
		if postErr := p.postFailure(channel, request.RootID, account, correlationID, failure); postErr != nil {
			p.API.LogError("Failed to post Doc2VLLM conversation error", "error", postErr, "correlation_id", correlationID)
		}
		return &BotRunResult{
			CorrelationID: correlationID,
			BotID:         account.Definition.ID,
			BotUsername:   account.Definition.Username,
			BotName:       account.Definition.DisplayName,
			Model:         account.Definition.Model,
			APIDurationMS: apiDuration.Milliseconds(),
			Status:        "failed",
			ErrorMessage:  failure.Message,
			ErrorCode:     failure.ErrorCode,
			ErrorDetail:   failure.Detail,
			ErrorHint:     failure.Hint,
			RequestURL:    failure.RequestURL,
			HTTPStatus:    failure.HTTPStatus,
			Retryable:     failure.Retryable,
		}, invokeErr
	}

	output := truncateString(strings.TrimSpace(extractDoc2VLLMResponseText(response)), cfg.MaxOutputLength)
	if bot.shouldMaskSensitiveData(cfg.MaskSensitiveData) {
		output = truncateString(maskSensitiveContent(output), cfg.MaxOutputLength)
	}
	post, err := p.postSuccess(channel, request.RootID, account, correlationID, output, successDebugView{
		Request: buildSuccessRequestDebugPayload([]doc2vllmRequestDebug{requestDebug}, ""),
	}, apiDuration)
	if err != nil {
		record := newExecutionRecord(request, account.Definition, correlationID, "failed", request.Prompt, err.Error(), "", true, startedAt, time.Now())
		p.appendExecutionHistory(request.UserID, record)
		return nil, err
	}

	state.Turns = append(state.Turns,
		conversationTurn{Role: "user", Content: request.Prompt},
		conversationTurn{Role: "assistant", Content: output},
	)
	if saveErr := p.saveThreadConversationState(*state); saveErr != nil {
		p.API.LogWarn("Failed to update Doc2VLLM thread conversation", "error", saveErr, "root_id", request.RootID, "correlation_id", correlationID)
	}

	record := newExecutionRecord(request, account.Definition, correlationID, "completed", request.Prompt, "", "", false, startedAt, time.Now())
	p.appendExecutionHistory(request.UserID, record)
	p.logUsage(cfg, correlationID, request, account.Definition, "completed", "")

	return &BotRunResult{
		CorrelationID: correlationID,
		BotID:         account.Definition.ID,
		BotUsername:   account.Definition.Username,
		BotName:       account.Definition.DisplayName,
		Model:         account.Definition.Model,
		APIDurationMS: apiDuration.Milliseconds(),
		PostID:        post.Id,
		Status:        "completed",
		Output:        output,
	}, nil
}

func buildDocumentResponseOutput(mode, prompt string, results []doc2vllmDocumentResult, failures []documentProcessingFailure, maxLength int) string {
	switch normalizeOutputMode(mode) {
	case "text":
		return buildDocumentResponseText(prompt, results, failures, maxLength)
	case "json":
		return buildDocumentResponseJSON(prompt, results, failures, maxLength)
	default:
		return buildDocumentResponseMarkdown(prompt, results, failures, maxLength)
	}
}

func buildDocumentResponseMarkdown(_ string, results []doc2vllmDocumentResult, failures []documentProcessingFailure, maxLength int) string {
	sections := make([]string, 0, len(results))
	for _, result := range results {
		contentFormat, content := buildRenderableDoc2VLLMContent(result.Response)
		if content == "" {
			content = "_추출된 텍스트가 없습니다._"
		}

		lines := []string{
			fmt.Sprintf("### %s", result.Attachment.Name),
			fmt.Sprintf("- Model: `%s`", defaultIfEmpty(strings.TrimSpace(result.Response.Model), defaultDoc2VLLMModel)),
		}
		if strings.TrimSpace(result.RequestPrompt) != "" {
			lines = append(lines, fmt.Sprintf("- Prompt: `%s`", truncateString(result.RequestPrompt, 120)))
		}
		if result.Response.Usage.PromptTokens > 0 {
			lines = append(lines, fmt.Sprintf("- Prompt Tokens: `%d`", result.Response.Usage.PromptTokens))
		}
		if result.Response.Usage.CompletionTokens > 0 {
			lines = append(lines, fmt.Sprintf("- Completion Tokens: `%d`", result.Response.Usage.CompletionTokens))
		}
		if result.Response.Usage.TotalTokens > 0 {
			lines = append(lines, fmt.Sprintf("- Total Tokens: `%d`", result.Response.Usage.TotalTokens))
		}
		if strings.TrimSpace(result.Source) != "" {
			lines = append(lines, fmt.Sprintf("- Source: `%s`", result.Source))
		}
		if strings.TrimSpace(result.Processor) != "" {
			lines = append(lines, fmt.Sprintf("- Processor: `%s`", result.Processor))
		}
		if contentFormat != "" {
			lines = append(lines, fmt.Sprintf("- Output: `%s`", contentFormat))
		}
		lines = append(lines, "", renderParsedContent(contentFormat, content))
		sections = append(sections, strings.Join(lines, "\n"))
	}

	if len(failures) > 0 {
		lines := []string{
			fmt.Sprintf("## Partial Failures (%d)", len(failures)),
		}
		for _, failure := range failures {
			lines = append(lines, renderDocumentFailureLine(failure))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}

	return truncateString(strings.TrimSpace(strings.Join(sections, "\n\n")), maxLength)
}

func buildDocumentResponseText(_ string, results []doc2vllmDocumentResult, failures []documentProcessingFailure, maxLength int) string {
	sections := make([]string, 0, len(results))
	for _, result := range results {
		_, content := buildRenderableDoc2VLLMContent(result.Response)
		lines := []string{
			fmt.Sprintf("[%s]", result.Attachment.Name),
		}
		if strings.TrimSpace(result.Source) != "" {
			lines = append(lines, fmt.Sprintf("source=%s", result.Source))
		}
		if strings.TrimSpace(result.Processor) != "" {
			lines = append(lines, fmt.Sprintf("processor=%s", result.Processor))
		}
		if strings.TrimSpace(content) == "" {
			lines = append(lines, "_추출된 텍스트가 없습니다._")
		} else {
			lines = append(lines, strings.TrimSpace(content))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	if len(failures) > 0 {
		lines := []string{fmt.Sprintf("[Partial Failures: %d]", len(failures))}
		for _, failure := range failures {
			lines = append(lines, renderDocumentFailureLine(failure))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	return truncateString(strings.TrimSpace(strings.Join(sections, "\n\n")), maxLength)
}

func buildDocumentResponseJSON(prompt string, results []doc2vllmDocumentResult, failures []documentProcessingFailure, maxLength int) string {
	type documentOutputItem struct {
		Name             string `json:"name"`
		Model            string `json:"model,omitempty"`
		Source           string `json:"source,omitempty"`
		Processor        string `json:"processor,omitempty"`
		RequestPrompt    string `json:"request_prompt,omitempty"`
		OutputFormat     string `json:"output_format,omitempty"`
		Content          string `json:"content,omitempty"`
		PromptTokens     int    `json:"prompt_tokens,omitempty"`
		CompletionTokens int    `json:"completion_tokens,omitempty"`
		TotalTokens      int    `json:"total_tokens,omitempty"`
	}
	payload := struct {
		Prompt    string                      `json:"prompt,omitempty"`
		Documents []documentOutputItem        `json:"documents"`
		Failures  []documentProcessingFailure `json:"failures,omitempty"`
	}{
		Prompt:    strings.TrimSpace(prompt),
		Documents: make([]documentOutputItem, 0, len(results)),
		Failures:  failures,
	}

	for _, result := range results {
		contentFormat, content := buildRenderableDoc2VLLMContent(result.Response)
		payload.Documents = append(payload.Documents, documentOutputItem{
			Name:             result.Attachment.Name,
			Model:            strings.TrimSpace(result.Response.Model),
			Source:           strings.TrimSpace(result.Source),
			Processor:        strings.TrimSpace(result.Processor),
			RequestPrompt:    strings.TrimSpace(result.RequestPrompt),
			OutputFormat:     contentFormat,
			Content:          strings.TrimSpace(content),
			PromptTokens:     result.Response.Usage.PromptTokens,
			CompletionTokens: result.Response.Usage.CompletionTokens,
			TotalTokens:      result.Response.Usage.TotalTokens,
		})
	}

	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return ""
	}
	return truncateString(string(raw), maxLength)
}

func renderDocumentFailureLine(failure documentProcessingFailure) string {
	line := fmt.Sprintf("- `%s`: %s", defaultIfEmpty(strings.TrimSpace(failure.AttachmentName), "attachment"), defaultIfEmpty(strings.TrimSpace(failure.Message), "처리에 실패했습니다."))
	if strings.TrimSpace(failure.Hint) != "" {
		line += fmt.Sprintf(" | 조치: %s", strings.TrimSpace(failure.Hint))
	}
	return line
}

func summarizeDocumentFailureMessages(failures []documentProcessingFailure, limit int) string {
	if len(failures) == 0 {
		return ""
	}

	if limit <= 0 || limit > len(failures) {
		limit = len(failures)
	}

	parts := make([]string, 0, limit+1)
	for _, failure := range failures[:limit] {
		parts = append(parts, renderDocumentFailureLine(failure))
	}
	if len(failures) > limit {
		parts = append(parts, fmt.Sprintf("... 외 %d건", len(failures)-limit))
	}
	return strings.Join(parts, "\n")
}

func buildExecutionFailureFromDocumentFailures(failures []documentProcessingFailure, apiDuration time.Duration) executionFailureView {
	if len(failures) == 0 {
		return executionFailureView{
			HasFailure:  true,
			StageLabel:  "문서 처리",
			Message:     "처리할 수 있는 첨부 파일이 없습니다.",
			APIDuration: apiDuration,
		}
	}
	if len(failures) == 1 {
		failure := failures[0]
		return executionFailureView{
			HasFailure:  true,
			StageLabel:  "문서 처리",
			Message:     defaultIfEmpty(strings.TrimSpace(failure.Message), "문서 처리에 실패했습니다."),
			ErrorCode:   strings.TrimSpace(failure.ErrorCode),
			Detail:      strings.TrimSpace(failure.Detail),
			Hint:        strings.TrimSpace(failure.Hint),
			HTTPStatus:  failure.HTTPStatus,
			Retryable:   failure.Retryable,
			APIDuration: apiDuration,
		}
	}

	retryable := false
	for _, failure := range failures {
		if failure.Retryable {
			retryable = true
			break
		}
	}

	return executionFailureView{
		HasFailure:  true,
		StageLabel:  "문서 처리",
		Message:     "모든 첨부 파일 처리에 실패했습니다.",
		ErrorCode:   "document_processing_failed",
		Detail:      summarizeDocumentFailureMessages(failures, 3),
		Hint:        "지원되는 파일 형식인지, 그리고 PDF 변환 도구가 서버에 설치되어 있는지 확인하세요.",
		Retryable:   retryable,
		APIDuration: apiDuration,
	}
}

func renderParsedContent(format, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	if format == "json" {
		return "```json\n" + value + "\n```"
	}
	return value
}

func buildRenderableDoc2VLLMContent(response doc2vllmOCRResponse) (string, string) {
	content := strings.TrimSpace(extractDoc2VLLMResponseText(response))
	if content == "" {
		pretty, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			return "", ""
		}
		return "json", string(pretty)
	}
	return "text", content
}

func (p *Plugin) ensureBots() error {
	cfg, err := p.getRuntimeConfiguration()
	if err != nil {
		p.setBotAccounts(map[string]botAccount{})
		p.setBotSyncState(botSyncState{
			LastError: err.Error(),
			UpdatedAt: time.Now().UnixMilli(),
			Entries:   []botSyncEntry{},
		})
		return err
	}
	if len(cfg.BotDefinitions) == 0 {
		p.setBotAccounts(map[string]botAccount{})
		deactivateErr := p.deactivateManagedBots(nil)
		lastError := ""
		if deactivateErr != nil {
			lastError = deactivateErr.Error()
		}
		p.setBotSyncState(botSyncState{
			LastError: lastError,
			UpdatedAt: time.Now().UnixMilli(),
			Entries:   []botSyncEntry{},
		})
		return nil
	}

	accounts := make(map[string]botAccount, len(cfg.BotDefinitions))
	syncEntries := make([]botSyncEntry, 0, len(cfg.BotDefinitions))
	configuredUsernames := make(map[string]struct{}, len(cfg.BotDefinitions))
	syncIssues := make([]string, 0)
	for _, definition := range cfg.BotDefinitions {
		configuredUsernames[definition.Username] = struct{}{}
		userID, statusMessage, ensureErr := p.ensureSingleBot(definition)
		entry := botSyncEntry{
			BotID:         definition.ID,
			Username:      definition.Username,
			DisplayName:   definition.DisplayName,
			Model:         definition.Model,
			UserID:        userID,
			Registered:    ensureErr == nil && userID != "",
			Active:        ensureErr == nil && userID != "",
			StatusMessage: statusMessage,
		}
		if ensureErr != nil {
			entry.StatusMessage = ensureErr.Error()
			entry.Active = false
			syncEntries = append(syncEntries, entry)
			syncIssues = append(syncIssues, ensureErr.Error())
			continue
		}
		accounts[definition.ID] = botAccount{
			Definition: definition,
			UserID:     userID,
		}
		syncEntries = append(syncEntries, entry)
	}

	if deactivateErr := p.deactivateManagedBots(configuredUsernames); deactivateErr != nil {
		syncIssues = append(syncIssues, deactivateErr.Error())
	}

	p.setBotAccounts(accounts)
	p.setBotSyncState(botSyncState{
		LastError: joinSyncIssues(syncIssues),
		UpdatedAt: time.Now().UnixMilli(),
		Entries:   syncEntries,
	})
	return nil
}

func (p *Plugin) ensureSingleBot(definition BotDefinition) (string, string, error) {
	description := botDescription(definition)
	displayName := definition.DisplayName

	existingUser, appErr := p.API.GetUserByUsername(definition.Username)
	if appErr == nil && existingUser != nil {
		if !existingUser.IsBot {
			return "", "", fmt.Errorf("username @%s is already used by a regular Mattermost account", definition.Username)
		}

		statusMessage := ""
		if _, err := p.client.Bot.Get(existingUser.Id, true); err == nil {
			if _, err := p.client.Bot.Patch(existingUser.Id, &model.BotPatch{
				DisplayName: &displayName,
				Description: &description,
			}); err != nil && !isBotNotFoundError(err) {
				return "", "", fmt.Errorf("failed to update Doc2VLLM bot @%s: %w", definition.Username, err)
			}
			if _, err := p.client.Bot.UpdateActive(existingUser.Id, true); err != nil && !isBotNotFoundError(err) {
				return "", "", fmt.Errorf("failed to activate Doc2VLLM bot @%s: %w", definition.Username, err)
			}
			p.API.LogInfo("Ensured Doc2VLLM OCR bot", "bot_username", definition.Username, "model", definition.Model, "action", "linked_existing")
			return existingUser.Id, statusMessage, nil
		}

		statusMessage = "기존 봇 사용자 계정을 연결했습니다. Bot 메타데이터 조회는 실패했지만 메시지 전송은 계속 시도합니다."
		p.API.LogWarn("Linked Doc2VLLM bot user without bot metadata", "bot_username", definition.Username, "user_id", existingUser.Id)
		return existingUser.Id, statusMessage, nil
	}

	if appErr != nil && appErr.StatusCode != http.StatusNotFound {
		return "", "", fmt.Errorf("failed to look up Mattermost user @%s: %w", definition.Username, appErr)
	}

	newBot := &model.Bot{
		Username:    definition.Username,
		DisplayName: definition.DisplayName,
		Description: description,
	}
	if err := p.client.Bot.Create(newBot); err != nil {
		existingUser, existingErr := p.API.GetUserByUsername(definition.Username)
		if existingErr == nil && existingUser != nil && existingUser.IsBot {
			p.API.LogWarn("Recovered Doc2VLLM bot by linking an already existing bot user", "bot_username", definition.Username, "user_id", existingUser.Id, "error", err.Error())
			return existingUser.Id, "이미 존재하는 봇 사용자 계정에 연결했습니다.", nil
		}
		return "", "", fmt.Errorf("failed to create Doc2VLLM bot @%s: %w", definition.Username, err)
	}

	p.API.LogInfo("Ensured Doc2VLLM OCR bot", "bot_username", definition.Username, "model", definition.Model, "action", "created")
	return newBot.UserId, "", nil
}

func (p *Plugin) deactivateManagedBots(configuredUsernames map[string]struct{}) error {
	bots, err := p.client.Bot.List(0, 200, pluginapi.BotOwner(manifest.Id))
	if err != nil {
		return fmt.Errorf("failed to list plugin bots for deactivation: %w", err)
	}

	issues := make([]string, 0)
	for _, bot := range bots {
		if bot == nil {
			continue
		}
		if _, keep := configuredUsernames[strings.ToLower(bot.Username)]; keep {
			continue
		}
		if _, err := p.client.Bot.UpdateActive(bot.UserId, false); err != nil {
			if isBotNotFoundError(err) {
				p.API.LogWarn("Skipped deactivation for missing Doc2VLLM bot metadata", "bot_username", bot.Username, "user_id", bot.UserId, "error", err.Error())
				continue
			}
			issues = append(issues, fmt.Sprintf("failed to deactivate removed Doc2VLLM bot @%s: %s", bot.Username, err.Error()))
			continue
		}
		p.API.LogInfo("Deactivated removed Doc2VLLM bot", "bot_username", bot.Username, "user_id", bot.UserId)
	}

	if len(issues) > 0 {
		return fmt.Errorf("%s", strings.Join(issues, "; "))
	}
	return nil
}

func (p *Plugin) ensureBotInChannel(channelID, botUserID string) error {
	if channelID == "" || botUserID == "" {
		return nil
	}
	if _, appErr := p.API.GetChannelMember(channelID, botUserID); appErr == nil {
		return nil
	}
	if _, appErr := p.API.AddUserToChannel(channelID, botUserID, ""); appErr != nil {
		return fmt.Errorf("failed to add bot to channel: %w", appErr)
	}
	return nil
}

func (p *Plugin) postSuccess(channel *model.Channel, rootID string, account botAccount, correlationID, output string, debugView successDebugView, apiDuration time.Duration) (*model.Post, error) {
	if err := p.ensureBotInChannel(channel.Id, account.UserID); err != nil {
		return nil, err
	}

	props := map[string]any{
		"from_bot":                 "true",
		"doc2vllm_bot_id":          account.Definition.ID,
		"doc2vllm_correlation_id":  correlationID,
		"doc2vllm_api_duration_ms": apiDuration.Milliseconds(),
		"doc2vllm_model":           account.Definition.Model,
		"doc2vllm_ocr":             "true",
	}
	if strings.TrimSpace(debugView.Request) != "" {
		props["doc2vllm_request_input"] = debugView.Request
	}
	if strings.TrimSpace(debugView.Output) != "" {
		props["doc2vllm_response_output"] = debugView.Output
	}

	post, appErr := p.API.CreatePost(&model.Post{
		UserId:    account.UserID,
		ChannelId: channel.Id,
		RootId:    rootID,
		Type:      doc2vllmBotPostType,
		Message:   buildBotResponseMessage(output, correlationID, apiDuration),
		Props:     props,
	})
	if appErr != nil {
		return nil, fmt.Errorf("failed to create Doc2VLLM response post: %w", appErr)
	}
	return post, nil
}

func buildVLLMFallbackOutput(documentContext, notice string) string {
	parts := []string{
		"_" + strings.TrimSpace(notice) + "_",
		"",
		strings.TrimSpace(documentContext),
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func (p *Plugin) postFailure(channel *model.Channel, rootID string, account botAccount, correlationID string, failure executionFailureView) error {
	if err := p.ensureBotInChannel(channel.Id, account.UserID); err != nil {
		return err
	}

	_, appErr := p.API.CreatePost(&model.Post{
		UserId:    account.UserID,
		ChannelId: channel.Id,
		RootId:    rootID,
		Type:      doc2vllmBotPostType,
		Message:   buildBotFailureMessage(account.Definition, correlationID, failure),
		Props: map[string]any{
			"from_bot":                 "true",
			"doc2vllm_bot_id":          account.Definition.ID,
			"doc2vllm_correlation_id":  correlationID,
			"doc2vllm_api_duration_ms": failure.APIDuration.Milliseconds(),
			"doc2vllm_model":           account.Definition.Model,
			"doc2vllm_error":           "true",
			"doc2vllm_error_code":      failure.ErrorCode,
			"doc2vllm_error_input":     failure.InputDebug,
			"doc2vllm_error_output":    failure.OutputDebug,
			"doc2vllm_ocr":             "true",
		},
	})
	if appErr != nil {
		return fmt.Errorf("failed to create Doc2VLLM error post: %w", appErr)
	}
	return nil
}

func (p *Plugin) postInstruction(channel *model.Channel, rootID string, account botAccount, message string) error {
	if channel == nil || strings.TrimSpace(message) == "" {
		return nil
	}
	if err := p.ensureBotInChannel(channel.Id, account.UserID); err != nil {
		return err
	}

	_, appErr := p.API.CreatePost(&model.Post{
		UserId:    account.UserID,
		ChannelId: channel.Id,
		RootId:    rootID,
		Type:      doc2vllmBotPostType,
		Message:   strings.TrimSpace(message),
		Props: map[string]any{
			"from_bot":        "true",
			"doc2vllm_bot_id": account.Definition.ID,
			"doc2vllm_ocr":    "true",
		},
	})
	if appErr != nil {
		return fmt.Errorf("failed to create Doc2VLLM instruction post: %w", appErr)
	}
	return nil
}

func responseRootID(post *model.Post) string {
	if post == nil {
		return ""
	}
	if post.RootId != "" {
		return post.RootId
	}
	return post.Id
}

func (p *Plugin) logUsage(cfg *runtimeConfiguration, correlationID string, request BotRunRequest, bot BotDefinition, status, errorMessage string) {
	if !cfg.EnableUsageLogs {
		return
	}
	p.API.LogInfo("Doc2VLLM OCR execution", "correlation_id", correlationID, "bot_id", bot.ID, "bot_username", bot.Username, "model", bot.Model, "user_id", request.UserID, "channel_id", request.ChannelID, "source", request.Source, "status", status, "error", errorMessage, "attachment_count", len(request.FileIDs))
}

func botDescription(bot BotDefinition) string {
	description := strings.TrimSpace(bot.Description)
	if description != "" {
		return description
	}
	return fmt.Sprintf("Doc2VLLM OCR bot using %s", bot.Model)
}

func buildBotResponseMessage(output, correlationID string, apiDuration time.Duration) string {
	body := strings.TrimSpace(output)
	if body == "" {
		body = "_빈 응답이 반환되었습니다._"
	}

	lines := []string{
		body,
		"",
		fmt.Sprintf("_Correlation ID:_ `%s`", correlationID),
	}
	if apiDuration > 0 {
		lines = append(lines, fmt.Sprintf("_Doc2VLLM API 응답 시간:_ `%s`", formatDoc2VLLMAPIDuration(apiDuration)))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func describeExecutionFailure(err error, defaultRetryable bool, apiDuration time.Duration) executionFailureView {
	if err == nil {
		return executionFailureView{}
	}

	var callErr *doc2vllmCallError
	if errors.As(err, &callErr) {
		return executionFailureView{
			HasFailure:  true,
			StageLabel:  "Doc2VLLM OCR",
			Message:     callErr.Error(),
			ErrorCode:   callErr.Code,
			Detail:      callErr.Detail,
			Hint:        callErr.Hint,
			RequestURL:  callErr.RequestURL,
			HTTPStatus:  callErr.StatusCode,
			Retryable:   callErr.Retryable,
			InputDebug:  callErr.InputDebug,
			OutputDebug: callErr.OutputDebug,
			APIDuration: apiDuration,
		}
	}

	var vllmErr *vllmCallError
	if errors.As(err, &vllmErr) {
		return executionFailureView{
			HasFailure:  true,
			StageLabel:  "vLLM 후처리",
			Message:     vllmErr.Error(),
			ErrorCode:   vllmErr.Code,
			Detail:      vllmErr.Detail,
			Hint:        vllmErr.Hint,
			RequestURL:  vllmErr.RequestURL,
			HTTPStatus:  vllmErr.StatusCode,
			Retryable:   vllmErr.Retryable,
			InputDebug:  vllmErr.InputDebug,
			OutputDebug: vllmErr.OutputDebug,
			APIDuration: apiDuration,
		}
	}

	return executionFailureView{
		HasFailure:  true,
		Message:     strings.TrimSpace(err.Error()),
		Retryable:   defaultRetryable,
		APIDuration: apiDuration,
	}
}

func buildBotFailureMessage(bot BotDefinition, correlationID string, failure executionFailureView) string {
	modelLabel := bot.Model
	if strings.Contains(failure.StageLabel, "vLLM") && strings.TrimSpace(bot.VLLMModel) != "" {
		modelLabel = bot.VLLMModel
	}
	lines := []string{
		fmt.Sprintf("%s 호출에 실패했습니다. 모델: `%s`", defaultIfEmpty(strings.TrimSpace(failure.StageLabel), "Doc2VLLM bot 실행"), modelLabel),
	}

	if failure.Message != "" {
		lines = append(lines, "", failure.Message)
	}
	if failure.Detail != "" && !strings.Contains(failure.Message, "상세: "+failure.Detail) {
		lines = append(lines, "", "상세: "+failure.Detail)
	}
	if failure.Hint != "" && !strings.Contains(failure.Message, "조치: "+failure.Hint) {
		lines = append(lines, "", "조치: "+failure.Hint)
	}
	if failure.HTTPStatus > 0 && !strings.Contains(failure.Message, "HTTP 상태:") {
		lines = append(lines, "", fmt.Sprintf("HTTP 상태: `%d`", failure.HTTPStatus))
	}
	if failure.Retryable {
		lines = append(lines, "", "_재시도 가능:_ 예")
	}
	if failure.InputDebug != "" || failure.OutputDebug != "" {
		lines = append(lines, "", "_상단 버튼에서 요청/응답 파라미터를 볼 수 있습니다._")
	}
	lines = append(lines, "", fmt.Sprintf("_Correlation ID:_ `%s`", correlationID))
	if failure.APIDuration > 0 {
		lines = append(lines, fmt.Sprintf("_Doc2VLLM API 응답 시간:_ `%s`", formatDoc2VLLMAPIDuration(failure.APIDuration)))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func buildSuccessRequestDebugPayload(doc2vllmRequestDebugs []doc2vllmRequestDebug, vllmRequestDebug string) string {
	payload := map[string]any{}
	if doc2vllmDebug := marshalDoc2VLLMRequestDebugs(doc2vllmRequestDebugs); doc2vllmDebug != "" {
		var parsed any
		if err := json.Unmarshal([]byte(doc2vllmDebug), &parsed); err == nil {
			payload["doc2vllm"] = parsed
		}
	}
	if strings.TrimSpace(vllmRequestDebug) != "" {
		var parsed any
		if err := json.Unmarshal([]byte(vllmRequestDebug), &parsed); err == nil {
			payload["vllm"] = parsed
		}
	}
	if len(payload) == 0 {
		return ""
	}
	return marshalDebugPayload(payload)
}

func formatDoc2VLLMAPIDuration(duration time.Duration) string {
	if duration <= 0 {
		return "0.00초"
	}

	seconds := duration.Seconds()
	switch {
	case seconds < 10:
		return fmt.Sprintf("%.2f초", seconds)
	case seconds < 100:
		return fmt.Sprintf("%.1f초", seconds)
	default:
		return fmt.Sprintf("%.0f초", seconds)
	}
}

func isBotNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "resource bot not found") ||
		strings.Contains(lower, "bot does not exist") ||
		strings.Contains(lower, "unable to get bot")
}

func joinSyncIssues(issues []string) string {
	filtered := make([]string, 0, len(issues))
	for _, issue := range issues {
		issue = strings.TrimSpace(issue)
		if issue == "" {
			continue
		}
		filtered = append(filtered, issue)
	}
	return strings.Join(filtered, " | ")
}
