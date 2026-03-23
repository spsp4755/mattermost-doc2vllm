package main

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultDoc2VLLMOCRPrompt = "Extract the visible text from the attached document faithfully without inventing missing content."
)

const defaultDoc2VLLMFollowupPrompt = "Please answer using the extracted document context."
const defaultDoc2VLLMAttachmentUserPrompt = "Please process the attached document according to the system instructions."
const defaultDoc2VLLMMultimodalPrompt = "Analyze the attached image or document and answer using only its visible contents."

type doc2vllmServiceConfig struct {
	BaseURL       string
	ParsedBaseURL *url.URL
	AuthMode      string
	AuthToken     string
	Timeout       time.Duration
}

type doc2vllmConnectionStatus struct {
	OK         bool   `json:"ok"`
	URL        string `json:"url"`
	StatusCode int    `json:"status_code"`
	Message    string `json:"message"`
	BotID      string `json:"bot_id,omitempty"`
	BotName    string `json:"bot_name,omitempty"`
	Model      string `json:"model,omitempty"`
	Mode       string `json:"mode,omitempty"`
	AuthMode   string `json:"auth_mode,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Hint       string `json:"hint,omitempty"`
	Retryable  bool   `json:"retryable"`
}

type doc2vllmChatRequest struct {
	Model       string            `json:"model"`
	Messages    []doc2vllmMessage `json:"messages"`
	Stream      bool              `json:"stream,omitempty"`
	Temperature float64           `json:"temperature"`
	MaxTokens   int               `json:"max_tokens"`
	TopP        float64           `json:"top_p"`
}

type doc2vllmMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type doc2vllmContentPart struct {
	Type     string                `json:"type"`
	Text     string                `json:"text,omitempty"`
	ImageURL *doc2vllmImageURLPart `json:"image_url,omitempty"`
}

type doc2vllmImageURLPart struct {
	URL string `json:"url"`
}

type doc2vllmUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type doc2vllmChoiceMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type doc2vllmChoice struct {
	Index        int                   `json:"index"`
	Message      doc2vllmChoiceMessage `json:"message"`
	FinishReason string                `json:"finish_reason"`
}

type doc2vllmOCRResponse struct {
	ID      string           `json:"id"`
	Object  string           `json:"object"`
	Created int64            `json:"created"`
	Model   string           `json:"model"`
	Choices []doc2vllmChoice `json:"choices"`
	Usage   doc2vllmUsage    `json:"usage"`
}

type doc2vllmDocumentResult struct {
	Attachment    botAttachment
	RequestPrompt string
	Response      doc2vllmOCRResponse
	RequestDebugs []doc2vllmRequestDebug
	Source        string
	Processor     string
}

type doc2vllmCallError struct {
	Code        string
	Summary     string
	Detail      string
	Hint        string
	RequestURL  string
	StatusCode  int
	Retryable   bool
	InputDebug  string
	OutputDebug string
}

type doc2vllmRequestDebug struct {
	URL                 string                  `json:"url"`
	AuthMode            string                  `json:"auth_mode"`
	Model               string                  `json:"model"`
	Mode                string                  `json:"mode,omitempty"`
	Prompt              string                  `json:"prompt"`
	SystemPrompt        string                  `json:"system_prompt,omitempty"`
	UserPrompt          string                  `json:"user_prompt,omitempty"`
	EffectiveUserPrompt string                  `json:"effective_user_prompt,omitempty"`
	Temperature         float64                 `json:"temperature"`
	MaxTokens           int                     `json:"max_tokens"`
	TopP                float64                 `json:"top_p"`
	RepetitionPenalty   float64                 `json:"repetition_penalty,omitempty"`
	PresencePenalty     float64                 `json:"presence_penalty,omitempty"`
	FrequencyPenalty    float64                 `json:"frequency_penalty,omitempty"`
	ExtraRequestJSON    string                  `json:"extra_request_json,omitempty"`
	Messages            []doc2vllmMessageDebug  `json:"messages,omitempty"`
	Attachment          doc2vllmAttachmentDebug `json:"attachment"`
	Correlation         string                  `json:"correlation_id,omitempty"`
}

type doc2vllmAttachmentDebug struct {
	Name      string `json:"name"`
	MIMEType  string `json:"mime_type"`
	Extension string `json:"extension,omitempty"`
	Size      int64  `json:"size"`
}

type doc2vllmMessageDebug struct {
	Role           string `json:"role"`
	ContentType    string `json:"content_type"`
	ContentPreview string `json:"content_preview,omitempty"`
}

type doc2vllmResponseDebug struct {
	StatusCode int    `json:"status_code,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	Summary    string `json:"summary,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Hint       string `json:"hint,omitempty"`
	Body       string `json:"body,omitempty"`
}

func (e *doc2vllmCallError) Error() string {
	if e == nil {
		return ""
	}

	lines := []string{}
	if e.Summary != "" {
		lines = append(lines, e.Summary)
	}
	if e.Detail != "" {
		lines = append(lines, "Detail: "+e.Detail)
	}
	if e.Hint != "" {
		lines = append(lines, "Hint: "+e.Hint)
	}
	if e.StatusCode > 0 {
		lines = append(lines, fmt.Sprintf("HTTP Status: %d", e.StatusCode))
	}

	return strings.Join(lines, "\n")
}

func (e *doc2vllmCallError) toConnectionStatus() *doc2vllmConnectionStatus {
	if e == nil {
		return &doc2vllmConnectionStatus{}
	}

	return &doc2vllmConnectionStatus{
		OK:         false,
		URL:        e.RequestURL,
		StatusCode: e.StatusCode,
		Message:    e.Summary,
		ErrorCode:  e.Code,
		Detail:     e.Detail,
		Hint:       e.Hint,
		Retryable:  e.Retryable,
	}
}

func normalizeDoc2VLLMEndpointURL(raw string) (string, *url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = defaultDoc2VLLMEndpointURL
	}

	parsedURL, err := url.Parse(raw)
	if err != nil {
		return "", nil, fmt.Errorf("invalid Doc2VLLM endpoint URL: %w", err)
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return "", nil, fmt.Errorf("Doc2VLLM endpoint URL must include scheme and host")
	}

	path := strings.TrimRight(parsedURL.Path, "/")
	switch {
	case path == "", path == "/":
		parsedURL.Path = "/v1/chat/completions"
	case path == "/v1":
		parsedURL.Path = "/v1/chat/completions"
	case strings.HasSuffix(path, "/chat/completions"):
		parsedURL.Path = path
	default:
		parsedURL.Path = path + "/chat/completions"
	}

	return parsedURL.String(), parsedURL, nil
}

func (cfg *runtimeConfiguration) serviceConfigForBot(bot BotDefinition) (doc2vllmServiceConfig, error) {
	baseURL := strings.TrimSpace(bot.BaseURL)
	if baseURL == "" {
		baseURL = cfg.ServiceBaseURL
	}
	normalizedURL, parsedURL, err := normalizeDoc2VLLMEndpointURL(baseURL)
	if err != nil {
		return doc2vllmServiceConfig{}, err
	}
	if !hostAllowed(parsedURL.Hostname(), cfg.AllowHosts) {
		return doc2vllmServiceConfig{}, fmt.Errorf("Doc2VLLM host %q is not allowed by configuration", parsedURL.Hostname())
	}

	authMode := normalizeAuthMode(bot.AuthMode)
	if strings.TrimSpace(bot.AuthMode) == "" {
		authMode = cfg.AuthMode
	}
	authToken := strings.TrimSpace(bot.AuthToken)
	if authToken == "" {
		authToken = cfg.AuthToken
	}

	return doc2vllmServiceConfig{
		BaseURL:       normalizedURL,
		ParsedBaseURL: parsedURL,
		AuthMode:      authMode,
		AuthToken:     authToken,
		Timeout:       cfg.DefaultTimeout,
	}, nil
}

func (p *Plugin) invokeDoc2VLLMOCR(
	ctx context.Context,
	service doc2vllmServiceConfig,
	bot BotDefinition,
	attachment botAttachment,
	userPrompt string,
	correlationID string,
) (doc2vllmDocumentResult, int, time.Duration, error) {
	requestPayload, requestDebug, requestPrompt, err := buildDoc2VLLMChatRequest(service, bot, &attachment, userPrompt, "", nil, correlationID)
	if err != nil {
		return doc2vllmDocumentResult{}, 0, 0, err
	}

	startedAt := time.Now()
	result, statusCode, err := p.performDoc2VLLMRequest(ctx, service, bot, attachment, requestPayload, requestDebug)
	elapsed := time.Since(startedAt)
	if err != nil {
		return result, statusCode, elapsed, err
	}

	result.RequestPrompt = requestPrompt
	result.RequestDebugs = append(result.RequestDebugs, requestDebug)
	return result, statusCode, elapsed, nil
}

func (p *Plugin) invokeDoc2VLLMOCRStream(
	ctx context.Context,
	service doc2vllmServiceConfig,
	bot BotDefinition,
	attachment botAttachment,
	userPrompt string,
	correlationID string,
	onSnapshot func(string) error,
) (doc2vllmDocumentResult, int, time.Duration, error) {
	requestPayload, requestDebug, requestPrompt, err := buildDoc2VLLMChatRequest(service, bot, &attachment, userPrompt, "", nil, correlationID)
	if err != nil {
		return doc2vllmDocumentResult{}, 0, 0, err
	}
	requestPayload.Stream = true

	startedAt := time.Now()
	result, statusCode, err := p.performDoc2VLLMStreamRequest(ctx, service, bot, attachment, requestPayload, requestDebug, onSnapshot)
	elapsed := time.Since(startedAt)
	if err != nil {
		return result, statusCode, elapsed, err
	}

	result.RequestPrompt = requestPrompt
	result.RequestDebugs = append(result.RequestDebugs, requestDebug)
	return result, statusCode, elapsed, nil
}

func buildDoc2VLLMChatRequest(
	service doc2vllmServiceConfig,
	bot BotDefinition,
	attachment *botAttachment,
	userPrompt string,
	documentContext string,
	turns []conversationTurn,
	correlationID string,
) (doc2vllmChatRequest, doc2vllmRequestDebug, string, error) {
	rawUserPrompt := strings.TrimSpace(userPrompt)
	hasAttachment := attachment != nil
	requestPrompt := rawUserPrompt
	effectiveUserPrompt := rawUserPrompt
	if hasAttachment {
		requestPrompt = bot.effectiveDoc2VLLMPrompt(rawUserPrompt)
		effectiveUserPrompt = bot.effectiveAttachmentUserPrompt(rawUserPrompt)
	} else if effectiveUserPrompt == "" {
		effectiveUserPrompt = defaultDoc2VLLMFollowupPrompt
	}

	messages := make([]doc2vllmMessage, 0, 2)
	systemPrompt := buildDoc2VLLMSystemPrompt(bot, documentContext, hasAttachment)
	if systemPrompt != "" {
		messages = append(messages, doc2vllmMessage{
			Role:    "system",
			Content: systemPrompt,
		})
	}

	debugAttachment := botAttachment{}
	if hasAttachment {
		userContent := []doc2vllmContentPart{{
			Type: "text",
			Text: buildDoc2VLLMUserPrompt(rawUserPrompt, effectiveUserPrompt, true),
		}}
		userMessage := doc2vllmMessage{
			Role:    "user",
			Content: userContent[0].Text,
		}
		dataURL, err := buildDoc2VLLMImageDataURL(*attachment)
		if err != nil {
			return doc2vllmChatRequest{}, doc2vllmRequestDebug{}, "", err
		}
		userContent = append(userContent, doc2vllmContentPart{
			Type: "image_url",
			ImageURL: &doc2vllmImageURLPart{
				URL: dataURL,
			},
		})
		debugAttachment = *attachment
		userMessage.Content = userContent
		messages = append(messages, userMessage)
	} else {
		messages = append(messages, buildDoc2VLLMConversationMessages(turns)...)
		messages = append(messages, doc2vllmMessage{
			Role:    "user",
			Content: buildDoc2VLLMUserPrompt(rawUserPrompt, effectiveUserPrompt, false),
		})
	}

	requestPayload := doc2vllmChatRequest{
		Model:       defaultIfEmpty(strings.TrimSpace(bot.Model), defaultDoc2VLLMModel),
		Messages:    messages,
		Temperature: bot.effectiveDoc2VLLMTemperature(),
		MaxTokens:   bot.effectiveDoc2VLLMMaxTokens(),
		TopP:        bot.effectiveDoc2VLLMTopP(),
	}

	return requestPayload, buildDoc2VLLMRequestDebug(service, bot, requestPayload, debugAttachment, systemPrompt, requestPrompt, rawUserPrompt, effectiveUserPrompt, correlationID), requestPrompt, nil
}

func buildDoc2VLLMSystemPrompt(bot BotDefinition, documentContext string, hasAttachment bool) string {
	if hasAttachment {
		assistantRole := "You are an OCR document assistant."
		if bot.effectiveMode() == "multimodal" {
			assistantRole = "You are a multimodal document assistant."
		}
		parts := []string{
			assistantRole,
		}
		if instruction := strings.TrimSpace(bot.effectiveOCRInstruction()); instruction != "" {
			parts = append(parts, instruction)
		}
		parts = append(parts, "Read the attached document carefully and answer in the same language as the user when possible.")
		return strings.TrimSpace(strings.Join(parts, "\n\n"))
	}

	parts := []string{
		"You are a document question-answering assistant.",
		"Use the extracted document context below as the source of truth for follow-up answers.",
		"If the answer is not grounded in the extracted document context, say so clearly.",
		"Answer only the current request. Do not repeat the full OCR transcript, the entire document, or the full question unless the user explicitly asks for it.",
	}
	if source := strings.TrimSpace(documentContext); source != "" {
		parts = append(parts, "[OCR document source]\n"+source)
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func buildDoc2VLLMUserPrompt(userPrompt, effectiveUserPrompt string, hasAttachment bool) string {
	userPrompt = strings.TrimSpace(userPrompt)
	effectiveUserPrompt = strings.TrimSpace(effectiveUserPrompt)
	if hasAttachment {
		if userPrompt != "" {
			return userPrompt
		}
		if effectiveUserPrompt != "" {
			return effectiveUserPrompt
		}
		return defaultDoc2VLLMAttachmentUserPrompt
	}

	if userPrompt != "" {
		return userPrompt
	}
	if effectiveUserPrompt != "" {
		return effectiveUserPrompt
	}
	return defaultDoc2VLLMFollowupPrompt
}

func buildDoc2VLLMConversationMessages(turns []conversationTurn) []doc2vllmMessage {
	normalized := conversationTurnsForFollowup(turns)
	if len(normalized) == 0 {
		return nil
	}

	messages := make([]doc2vllmMessage, 0, len(normalized))
	for _, turn := range normalized {
		role := turn.Role
		switch role {
		case "assistant", "system":
		default:
			role = "user"
		}
		messages = append(messages, doc2vllmMessage{
			Role:    role,
			Content: turn.Content,
		})
	}

	return messages
}

func buildDoc2VLLMConversationHistory(turns []conversationTurn) string {
	normalized := conversationTurnsForFollowup(turns)
	if len(normalized) == 0 {
		return ""
	}

	lines := make([]string, 0, len(normalized))
	for _, turn := range normalized {
		roleLabel := "User"
		switch turn.Role {
		case "assistant":
			roleLabel = "Assistant"
		case "system":
			roleLabel = "System"
		}
		lines = append(lines, fmt.Sprintf("%s: %s", roleLabel, turn.Content))
	}
	return strings.Join(lines, "\n")
}

func (p *Plugin) invokeDoc2VLLMConversation(
	ctx context.Context,
	service doc2vllmServiceConfig,
	bot BotDefinition,
	documentContext string,
	turns []conversationTurn,
	userPrompt string,
	correlationID string,
) (doc2vllmOCRResponse, doc2vllmRequestDebug, time.Duration, int, error) {
	requestPayload, requestDebug, _, err := buildDoc2VLLMChatRequest(service, bot, nil, userPrompt, documentContext, turns, correlationID)
	if err != nil {
		return doc2vllmOCRResponse{}, doc2vllmRequestDebug{}, 0, 0, err
	}

	startedAt := time.Now()
	result, statusCode, err := p.performDoc2VLLMRequest(ctx, service, bot, botAttachment{}, requestPayload, requestDebug)
	elapsed := time.Since(startedAt)
	if err != nil {
		return doc2vllmOCRResponse{}, doc2vllmRequestDebug{}, elapsed, statusCode, err
	}
	return result.Response, requestDebug, elapsed, statusCode, nil
}

func (p *Plugin) invokeDoc2VLLMConversationStream(
	ctx context.Context,
	service doc2vllmServiceConfig,
	bot BotDefinition,
	documentContext string,
	turns []conversationTurn,
	userPrompt string,
	correlationID string,
	onSnapshot func(string) error,
) (doc2vllmOCRResponse, doc2vllmRequestDebug, time.Duration, int, error) {
	requestPayload, requestDebug, _, err := buildDoc2VLLMChatRequest(service, bot, nil, userPrompt, documentContext, turns, correlationID)
	if err != nil {
		return doc2vllmOCRResponse{}, doc2vllmRequestDebug{}, 0, 0, err
	}
	requestPayload.Stream = true

	startedAt := time.Now()
	result, statusCode, err := p.performDoc2VLLMStreamRequest(ctx, service, bot, botAttachment{}, requestPayload, requestDebug, onSnapshot)
	elapsed := time.Since(startedAt)
	if err != nil {
		return doc2vllmOCRResponse{}, doc2vllmRequestDebug{}, elapsed, statusCode, err
	}
	return result.Response, requestDebug, elapsed, statusCode, nil
}

func newDirectTextDocumentResult(attachment botAttachment, text, model, source, processor string) doc2vllmDocumentResult {
	return doc2vllmDocumentResult{
		Attachment: attachment,
		Response: doc2vllmOCRResponse{
			Model: defaultIfEmpty(strings.TrimSpace(model), "direct-text"),
			Choices: []doc2vllmChoice{{
				Index: 0,
				Message: doc2vllmChoiceMessage{
					Role:    "assistant",
					Content: text,
				},
				FinishReason: "stop",
			}},
		},
		Source:    strings.TrimSpace(source),
		Processor: strings.TrimSpace(processor),
	}
}

func newPDFTextDocumentResult(attachment botAttachment, text, processor string) doc2vllmDocumentResult {
	return newDirectTextDocumentResult(attachment, text, "pdf-text-layer", "pdf_text", processor)
}

func buildDoc2VLLMImageDataURL(attachment botAttachment) (string, error) {
	mimeType := strings.TrimSpace(attachment.MIMEType)
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return "", newDoc2VLLMCallError(
			"unsupported_media_type",
			"Doc2VLLM OCR currently supports image attachments only.",
			fmt.Sprintf("Current file type: %s", defaultIfEmpty(mimeType, "unknown")),
			"Attach an image file such as PNG, JPG, or WEBP.",
			"",
			0,
			false,
		)
	}
	if len(attachment.Content) == 0 {
		return "", newDoc2VLLMCallError(
			"empty_attachment",
			"Empty image attachments cannot be processed.",
			sanitizeUploadFilename(attachment.Name),
			"Check that the image file uploaded correctly.",
			"",
			0,
			false,
		)
	}

	encoded := base64.StdEncoding.EncodeToString(attachment.Content)
	return fmt.Sprintf("data:%s;base64,%s", mimeType, encoded), nil
}

func buildDoc2VLLMRequestBody(requestPayload doc2vllmChatRequest, bot BotDefinition) (map[string]any, error) {
	body := map[string]any{
		"model":       requestPayload.Model,
		"messages":    requestPayload.Messages,
		"temperature": requestPayload.Temperature,
		"max_tokens":  requestPayload.MaxTokens,
		"top_p":       requestPayload.TopP,
	}
	if requestPayload.Stream {
		body["stream"] = true
	}

	if bot.PresencePenalty != 0 {
		body["presence_penalty"] = bot.PresencePenalty
	}
	if bot.RepetitionPenalty > 0 && bot.RepetitionPenalty != 1 {
		body["repetition_penalty"] = bot.RepetitionPenalty
	}
	if bot.FrequencyPenalty != 0 {
		body["frequency_penalty"] = bot.FrequencyPenalty
	}

	extra, err := parseDoc2VLLMExtraRequestJSON(bot.ExtraRequestJSON)
	if err != nil {
		return nil, err
	}
	for key, value := range extra {
		body[key] = value
	}

	return body, nil
}

func parseDoc2VLLMExtraRequestJSON(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("invalid extra_request_json: %w", err)
	}
	if len(payload) == 0 {
		return nil, nil
	}

	reservedKeys := map[string]struct{}{
		"model":              {},
		"messages":           {},
		"temperature":        {},
		"max_tokens":         {},
		"top_p":              {},
		"repetition_penalty": {},
		"presence_penalty":   {},
		"frequency_penalty":  {},
	}
	for key := range payload {
		if _, reserved := reservedKeys[key]; reserved {
			return nil, fmt.Errorf("extra_request_json cannot override reserved field %q", key)
		}
	}

	return payload, nil
}

func (p *Plugin) performDoc2VLLMRequest(
	ctx context.Context,
	service doc2vllmServiceConfig,
	bot BotDefinition,
	attachment botAttachment,
	requestPayload doc2vllmChatRequest,
	requestDebug doc2vllmRequestDebug,
) (doc2vllmDocumentResult, int, error) {
	requestBody, err := buildDoc2VLLMRequestBody(requestPayload, bot)
	if err != nil {
		return doc2vllmDocumentResult{}, 0, err
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return doc2vllmDocumentResult{}, 0, fmt.Errorf("failed to encode Doc2VLLM OCR request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, service.BaseURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return doc2vllmDocumentResult{}, 0, fmt.Errorf("failed to build Doc2VLLM OCR request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Correlation-ID", strings.TrimSpace(requestDebug.Correlation))
	applyAuthHeader(request, service.AuthMode, service.AuthToken)

	client := &http.Client{Timeout: resolveDoc2VLLMRequestTimeout(service.Timeout)}
	response, err := client.Do(request)
	if err != nil {
		return doc2vllmDocumentResult{}, 0, attachDoc2VLLMDebug(
			classifyDoc2VLLMRequestError(service.BaseURL, err),
			requestDebug,
			doc2vllmResponseDebug{},
		)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024))
	if err != nil {
		callErr := newDoc2VLLMCallError(
			"response_read_failed",
			"Doc2VLLM ?묐떟 蹂몃Ц???쎈뒗 以??ㅻ쪟媛 諛쒖깮?덉뒿?덈떎.",
			err.Error(),
			"Doc2VLLM ?쒕쾭 ?곹깭? ?묐떟 ?ш린 ?쒗븳???뺤씤?섏꽭??",
			service.BaseURL,
			response.StatusCode,
			true,
		)
		return doc2vllmDocumentResult{}, response.StatusCode, callErr.withDebug(
			requestDebug,
			buildDoc2VLLMResponseDebug(response.StatusCode, response.Header, nil, callErr),
		)
	}
	if response.StatusCode >= http.StatusBadRequest {
		callErr := classifyDoc2VLLMHTTPError(service.BaseURL, response.StatusCode, response.Header, responseBody)
		return doc2vllmDocumentResult{}, response.StatusCode, attachDoc2VLLMDebug(
			callErr,
			requestDebug,
			buildDoc2VLLMResponseDebug(response.StatusCode, response.Header, responseBody, callErr),
		)
	}

	var parsed doc2vllmOCRResponse
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		callErr := newDoc2VLLMCallError(
			"decode_failed",
			"Doc2VLLM ?묐떟 JSON???댁꽍?섏? 紐삵뻽?듬땲??",
			err.Error(),
			"Doc2VLLM OpenAI ?명솚 ?붾뱶?ъ씤?몄? ?묐떟 ?뺤떇???뺤씤?섏꽭??",
			service.BaseURL,
			response.StatusCode,
			false,
		)
		return doc2vllmDocumentResult{}, response.StatusCode, callErr.withDebug(
			requestDebug,
			buildDoc2VLLMResponseDebug(response.StatusCode, response.Header, responseBody, callErr),
		)
	}
	if strings.TrimSpace(parsed.Model) == "" {
		parsed.Model = bot.Model
	}

	return doc2vllmDocumentResult{
		Attachment: attachment,
		Response:   parsed,
	}, response.StatusCode, nil
}

func (p *Plugin) performDoc2VLLMStreamRequest(
	ctx context.Context,
	service doc2vllmServiceConfig,
	bot BotDefinition,
	attachment botAttachment,
	requestPayload doc2vllmChatRequest,
	requestDebug doc2vllmRequestDebug,
	onSnapshot func(string) error,
) (doc2vllmDocumentResult, int, error) {
	requestBody, err := buildDoc2VLLMRequestBody(requestPayload, bot)
	if err != nil {
		return doc2vllmDocumentResult{}, 0, err
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return doc2vllmDocumentResult{}, 0, fmt.Errorf("failed to encode Doc2VLLM OCR request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, service.BaseURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return doc2vllmDocumentResult{}, 0, fmt.Errorf("failed to build Doc2VLLM OCR request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	request.Header.Set("Cache-Control", "no-cache")
	request.Header.Set("X-Correlation-ID", strings.TrimSpace(requestDebug.Correlation))
	applyAuthHeader(request, service.AuthMode, service.AuthToken)

	client := &http.Client{Timeout: resolveDoc2VLLMRequestTimeout(service.Timeout)}
	response, err := client.Do(request)
	if err != nil {
		return doc2vllmDocumentResult{}, 0, attachDoc2VLLMDebug(
			classifyDoc2VLLMRequestError(service.BaseURL, err),
			requestDebug,
			doc2vllmResponseDebug{},
		)
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusBadRequest {
		responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024))
		callErr := classifyDoc2VLLMHTTPError(service.BaseURL, response.StatusCode, response.Header, responseBody)
		return doc2vllmDocumentResult{}, response.StatusCode, attachDoc2VLLMDebug(
			callErr,
			requestDebug,
			buildDoc2VLLMResponseDebug(response.StatusCode, response.Header, responseBody, callErr),
		)
	}

	content, err := consumeOpenAITextStream(response.Body, onSnapshot)
	if err != nil {
		callErr := newDoc2VLLMCallError(
			"stream_decode_failed",
			"Doc2VLLM streaming 응답을 해석하지 못했습니다.",
			err.Error(),
			"stream 지원 여부와 OpenAI 호환 streaming 형식을 확인하세요.",
			service.BaseURL,
			response.StatusCode,
			true,
		)
		return doc2vllmDocumentResult{}, response.StatusCode, callErr.withDebug(
			requestDebug,
			buildDoc2VLLMResponseDebug(response.StatusCode, response.Header, nil, callErr),
		)
	}
	if strings.TrimSpace(content) == "" {
		callErr := newDoc2VLLMCallError(
			"empty_response",
			"Doc2VLLM streaming 응답이 비어 있습니다.",
			"streaming 응답에서 텍스트 조각을 찾지 못했습니다.",
			"모델의 stream 지원 여부를 확인하거나 일반 응답 방식으로 다시 시도하세요.",
			service.BaseURL,
			response.StatusCode,
			false,
		)
		return doc2vllmDocumentResult{}, response.StatusCode, callErr.withDebug(
			requestDebug,
			buildDoc2VLLMResponseDebug(response.StatusCode, response.Header, nil, callErr),
		)
	}

	return doc2vllmDocumentResult{
		Attachment: attachment,
		Response: doc2vllmOCRResponse{
			Model: defaultIfEmpty(strings.TrimSpace(bot.Model), defaultDoc2VLLMModel),
			Choices: []doc2vllmChoice{{
				Index: 0,
				Message: doc2vllmChoiceMessage{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: "stop",
			}},
		},
	}, response.StatusCode, nil
}

func extractDoc2VLLMResponseText(response doc2vllmOCRResponse) string {
	for _, choice := range response.Choices {
		if value := strings.TrimSpace(choice.Text()); value != "" {
			return value
		}
		if value := strings.TrimSpace(extractTextFromValue(choice.Message.Content)); value != "" {
			return value
		}
	}

	pretty, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return ""
	}
	return string(pretty)
}

func (c doc2vllmChoice) Text() string {
	if value := strings.TrimSpace(extractTextFromValue(c.Message.Content)); value != "" {
		return value
	}
	return ""
}

func (p *Plugin) testDoc2VLLMConnection(ctx context.Context, cfg *runtimeConfiguration, botID string) (*doc2vllmConnectionStatus, error) {
	testBot := BotDefinition{}
	if strings.TrimSpace(botID) != "" {
		selectedBot := cfg.getBotByID(botID)
		if selectedBot == nil {
			return nil, fmt.Errorf("bot %q was not found in the current configuration", strings.TrimSpace(botID))
		}
		testBot = *selectedBot
	}

	serviceConfig, err := cfg.serviceConfigForBot(testBot)
	if err != nil {
		return nil, err
	}

	model := defaultDoc2VLLMModel
	if strings.TrimSpace(testBot.Model) != "" {
		model = strings.TrimSpace(testBot.Model)
	}

	requestPayload := doc2vllmChatRequest{
		Model: model,
		Messages: []doc2vllmMessage{{
			Role:    "user",
			Content: defaultDoc2VLLMOCRPrompt,
		}},
		Temperature: 0,
		MaxTokens:   16,
		TopP:        1,
	}

	bodyBytes, err := json.Marshal(requestPayload)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, serviceConfig.BaseURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create Doc2VLLM connection test request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	applyAuthHeader(request, serviceConfig.AuthMode, serviceConfig.AuthToken)

	client := &http.Client{Timeout: minDuration(cfg.DefaultTimeout, 10*time.Second)}
	response, err := client.Do(request)
	if err != nil {
		return classifyDoc2VLLMRequestError(serviceConfig.BaseURL, err).toConnectionStatus(), nil
	}
	defer response.Body.Close()

	bodyBytes, _ = io.ReadAll(io.LimitReader(response.Body, 32*1024))
	if response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusUnsupportedMediaType || response.StatusCode == http.StatusUnprocessableEntity {
		return &doc2vllmConnectionStatus{
			OK:         true,
			URL:        serviceConfig.BaseURL,
			StatusCode: response.StatusCode,
			BotID:      strings.TrimSpace(testBot.ID),
			BotName:    defaultIfEmpty(strings.TrimSpace(testBot.DisplayName), strings.TrimSpace(testBot.Username)),
			Model:      model,
			Mode:       testBot.effectiveMode(),
			AuthMode:   serviceConfig.AuthMode,
			Message:    "Connection to the endpoint succeeded. The probe request was rejected only because it did not include a real OCR attachment.",
		}, nil
	}
	if response.StatusCode >= http.StatusBadRequest {
		status := classifyDoc2VLLMHTTPError(serviceConfig.BaseURL, response.StatusCode, response.Header, bodyBytes).toConnectionStatus()
		status.BotID = strings.TrimSpace(testBot.ID)
		status.BotName = defaultIfEmpty(strings.TrimSpace(testBot.DisplayName), strings.TrimSpace(testBot.Username))
		status.Model = model
		status.Mode = testBot.effectiveMode()
		status.AuthMode = serviceConfig.AuthMode
		return status, nil
	}

	return &doc2vllmConnectionStatus{
		OK:         true,
		URL:        serviceConfig.BaseURL,
		StatusCode: response.StatusCode,
		BotID:      strings.TrimSpace(testBot.ID),
		BotName:    defaultIfEmpty(strings.TrimSpace(testBot.DisplayName), strings.TrimSpace(testBot.Username)),
		Model:      model,
		Mode:       testBot.effectiveMode(),
		AuthMode:   serviceConfig.AuthMode,
		Message:    defaultIfEmpty(strings.TrimSpace(extractTextFromBody(bodyBytes)), "Connection succeeded."),
	}, nil
}

func applyAuthHeader(request *http.Request, authMode, authToken string) {
	if strings.TrimSpace(authToken) == "" {
		return
	}
	if strings.TrimSpace(authMode) == "x-api-key" {
		request.Header.Set("x-api-key", authToken)
		return
	}
	request.Header.Set("Authorization", "Bearer "+authToken)
}

func summarizeResponseBody(body []byte) string {
	text := extractTextFromBody(body)
	if text != "" {
		return truncateString(text, 280)
	}
	return truncateString(strings.TrimSpace(string(body)), 280)
}

func extractTextFromBody(body []byte) string {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return strings.TrimSpace(string(body))
	}

	text := extractTextFromValue(payload)
	if text != "" {
		return text
	}

	pretty, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return ""
	}
	return string(pretty)
}

func extractTextFromValue(value any) string {
	candidates := make([]string, 0, 8)
	collectTextCandidates(value, &candidates)

	best := ""
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if len(candidate) > len(best) {
			best = candidate
		}
	}

	return best
}

func collectTextCandidates(value any, candidates *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			lowerKey := strings.ToLower(key)
			if isLikelyTextKey(lowerKey) {
				switch nestedValue := nested.(type) {
				case string:
					*candidates = append(*candidates, nestedValue)
				case map[string]any, []any:
					collectTextCandidates(nestedValue, candidates)
				}
				continue
			}
			collectTextCandidates(nested, candidates)
		}
	case []any:
		for _, item := range typed {
			collectTextCandidates(item, candidates)
		}
	case string:
		if strings.TrimSpace(typed) != "" {
			*candidates = append(*candidates, typed)
		}
	}
}

func isLikelyTextKey(key string) bool {
	return strings.Contains(key, "text") ||
		strings.Contains(key, "message") ||
		strings.Contains(key, "output") ||
		strings.Contains(key, "result") ||
		strings.Contains(key, "content") ||
		strings.Contains(key, "response") ||
		strings.Contains(key, "detail") ||
		strings.Contains(key, "error")
}

func truncateString(value string, maxLength int) string {
	value = strings.TrimSpace(strings.ToValidUTF8(value, ""))
	if maxLength <= 0 || utf8.RuneCountInString(value) <= maxLength {
		return value
	}
	runes := []rune(value)
	if maxLength <= 3 {
		return string(runes[:maxLength])
	}
	return string(runes[:maxLength-3]) + "..."
}

func minDuration(values ...time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	minimum := values[0]
	for _, value := range values[1:] {
		if value < minimum {
			minimum = value
		}
	}
	return minimum
}

func resolveDoc2VLLMRequestTimeout(value time.Duration) time.Duration {
	if value <= 0 {
		return time.Duration(defaultTimeoutSeconds) * time.Second
	}
	return value
}

func buildDoc2VLLMRequestDebug(
	service doc2vllmServiceConfig,
	bot BotDefinition,
	requestPayload doc2vllmChatRequest,
	attachment botAttachment,
	systemPrompt string,
	requestPrompt string,
	userPrompt string,
	effectiveUserPrompt string,
	correlationID string,
) doc2vllmRequestDebug {
	return doc2vllmRequestDebug{
		URL:                 strings.TrimSpace(service.BaseURL),
		AuthMode:            strings.TrimSpace(service.AuthMode),
		Model:               strings.TrimSpace(requestPayload.Model),
		Mode:                strings.TrimSpace(bot.effectiveMode()),
		Prompt:              truncateString(strings.TrimSpace(requestPrompt), 2000),
		SystemPrompt:        truncateString(strings.TrimSpace(systemPrompt), 2000),
		UserPrompt:          truncateString(strings.TrimSpace(userPrompt), 2000),
		EffectiveUserPrompt: truncateString(strings.TrimSpace(effectiveUserPrompt), 2000),
		Temperature:         requestPayload.Temperature,
		MaxTokens:           requestPayload.MaxTokens,
		TopP:                requestPayload.TopP,
		RepetitionPenalty:   bot.RepetitionPenalty,
		PresencePenalty:     bot.PresencePenalty,
		FrequencyPenalty:    bot.FrequencyPenalty,
		ExtraRequestJSON:    truncateString(strings.TrimSpace(bot.ExtraRequestJSON), 2000),
		Messages:            buildDoc2VLLMMessageDebugs(requestPayload.Messages),
		Attachment: doc2vllmAttachmentDebug{
			Name:      sanitizeUploadFilename(attachment.Name),
			MIMEType:  strings.TrimSpace(attachment.MIMEType),
			Extension: strings.TrimPrefix(strings.TrimSpace(attachment.Extension), "."),
			Size:      int64(len(attachment.Content)),
		},
		Correlation: strings.TrimSpace(correlationID),
	}
}

func buildDoc2VLLMMessageDebugs(messages []doc2vllmMessage) []doc2vllmMessageDebug {
	if len(messages) == 0 {
		return nil
	}

	debugMessages := make([]doc2vllmMessageDebug, 0, len(messages))
	for _, message := range messages {
		contentType := "unknown"
		contentPreview := ""

		switch typed := message.Content.(type) {
		case string:
			contentType = "text"
			contentPreview = typed
		case []doc2vllmContentPart:
			contentType = "multimodal"
			textParts := make([]string, 0, len(typed))
			imageCount := 0
			for _, part := range typed {
				if strings.TrimSpace(part.Text) != "" {
					textParts = append(textParts, strings.TrimSpace(part.Text))
				}
				if part.Type == "image_url" && part.ImageURL != nil {
					imageCount++
				}
			}
			contentPreview = strings.Join(textParts, "\n")
			if imageCount > 0 {
				imageSummary := fmt.Sprintf("[images: %d]", imageCount)
				if contentPreview == "" {
					contentPreview = imageSummary
				} else {
					contentPreview += "\n" + imageSummary
				}
			}
		default:
			contentPreview = extractTextFromValue(typed)
			if contentPreview != "" {
				contentType = "structured"
			}
		}

		debugMessages = append(debugMessages, doc2vllmMessageDebug{
			Role:           message.Role,
			ContentType:    contentType,
			ContentPreview: truncateString(strings.TrimSpace(contentPreview), 600),
		})
	}

	return debugMessages
}

func buildDoc2VLLMResponseDebug(statusCode int, headers http.Header, body []byte, callErr *doc2vllmCallError) doc2vllmResponseDebug {
	responseDebug := doc2vllmResponseDebug{
		StatusCode: statusCode,
		RequestID:  firstHeaderValue(headers, "X-Request-Id", "X-Request-ID", "X-Correlation-ID"),
		Body:       formatDebugBody(body),
	}
	if callErr != nil {
		responseDebug.ErrorCode = strings.TrimSpace(callErr.Code)
		responseDebug.Summary = strings.TrimSpace(callErr.Summary)
		responseDebug.Detail = strings.TrimSpace(callErr.Detail)
		responseDebug.Hint = strings.TrimSpace(callErr.Hint)
	}
	return responseDebug
}

func formatDebugBody(body []byte) string {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return ""
	}

	var payload any
	if err := json.Unmarshal(trimmed, &payload); err == nil {
		pretty, marshalErr := json.MarshalIndent(payload, "", "  ")
		if marshalErr == nil {
			return truncateString(string(pretty), 16*1024)
		}
	}

	return truncateString(string(trimmed), 16*1024)
}

func attachDoc2VLLMDebug(err error, requestDebug doc2vllmRequestDebug, responseDebug doc2vllmResponseDebug) error {
	if err == nil {
		return nil
	}

	var callErr *doc2vllmCallError
	if errors.As(err, &callErr) {
		return callErr.withDebug(requestDebug, responseDebug)
	}
	return err
}

func attachDoc2VLLMAttemptDebug(err error, requestDebugs []doc2vllmRequestDebug) error {
	if err == nil {
		return nil
	}

	var callErr *doc2vllmCallError
	if !errors.As(err, &callErr) {
		return err
	}

	copyErr := *callErr
	copyErr.InputDebug = marshalDoc2VLLMRequestDebugs(requestDebugs)
	return &copyErr
}

func (e *doc2vllmCallError) withDebug(requestDebug doc2vllmRequestDebug, responseDebug doc2vllmResponseDebug) *doc2vllmCallError {
	if e == nil {
		return nil
	}

	copyErr := *e
	copyErr.InputDebug = marshalDebugPayload(requestDebug)
	copyErr.OutputDebug = marshalDebugPayload(responseDebug)
	return &copyErr
}

func marshalDebugPayload(payload any) string {
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return ""
	}
	return truncateString(string(raw), 16*1024)
}

func marshalDoc2VLLMRequestDebugs(requestDebugs []doc2vllmRequestDebug) string {
	switch len(requestDebugs) {
	case 0:
		return ""
	case 1:
		return marshalDebugPayload(requestDebugs[0])
	default:
		return marshalDebugPayload(requestDebugs)
	}
}

func newDoc2VLLMCallError(code, summary, detail, hint, requestURL string, statusCode int, retryable bool) *doc2vllmCallError {
	return &doc2vllmCallError{
		Code:       code,
		Summary:    strings.TrimSpace(summary),
		Detail:     strings.TrimSpace(detail),
		Hint:       strings.TrimSpace(hint),
		RequestURL: strings.TrimSpace(requestURL),
		StatusCode: statusCode,
		Retryable:  retryable,
	}
}

func classifyDoc2VLLMHTTPError(requestURL string, statusCode int, headers http.Header, body []byte) *doc2vllmCallError {
	bodySummary := summarizeResponseBody(body)
	requestID := firstHeaderValue(headers, "X-Request-Id", "X-Request-ID", "X-Correlation-ID")
	if requestID != "" {
		bodySummary = strings.TrimSpace(bodySummary + " (request id: " + requestID + ")")
	}

	switch statusCode {
	case http.StatusBadRequest:
		return newDoc2VLLMCallError(
			"bad_request",
			"Doc2VLLM OCR ?붿껌??嫄곕??섏뿀?듬땲??",
			defaultIfEmpty(bodySummary, "messages ?먮뒗 image_url ?뺤떇??Doc2VLLM ?붽뎄?ы빆怨?留욎? ?딆뒿?덈떎."),
			"model, messages, image_url.url, max_tokens 媛믪쓣 ?뺤씤?섏꽭??",
			requestURL,
			statusCode,
			false,
		)
	case http.StatusUnauthorized, http.StatusForbidden:
		return newDoc2VLLMCallError(
			"auth_failed",
			"Doc2VLLM authentication failed.",
			defaultIfEmpty(bodySummary, "The API key is invalid or does not have permission."),
			"Check the authentication token and header settings in System Console.",
			requestURL,
			statusCode,
			false,
		)
	case http.StatusNotFound:
		return newDoc2VLLMCallError(
			"not_found",
			"Doc2VLLM API ?붾뱶?ъ씤?몃? 李얠? 紐삵뻽?듬땲??",
			defaultIfEmpty(bodySummary, "chat/completions 寃쎈줈媛 ?щ컮瑜댁? ?딆뒿?덈떎."),
			"湲곕낯 URL??OpenAI ?명솚 chat completions ?붾뱶?ъ씤?몃? 媛由ы궎?붿? ?뺤씤?섏꽭??",
			requestURL,
			statusCode,
			false,
		)
	case http.StatusTooManyRequests:
		return newDoc2VLLMCallError(
			"rate_limited",
			"Doc2VLLM ?몄텧 ?쒕룄??嫄몃졇?듬땲??",
			defaultIfEmpty(bodySummary, "?좎떆 ???ㅼ떆 ?쒕룄?댁빞 ?⑸땲??"),
			"?붿껌 鍮덈룄瑜?以꾩씠嫄곕굹 ?좎떆 ???ㅼ떆 ?쒕룄?섏꽭??",
			requestURL,
			statusCode,
			true,
		)
	case http.StatusRequestEntityTooLarge:
		return newDoc2VLLMCallError(
			"image_too_large",
			"?낅줈?쒗븳 ?대?吏媛 ?덈Т ?쎈땲??",
			defaultIfEmpty(bodySummary, "Doc2VLLM???대?吏 ?ш린 ?쒗븳??珥덇낵???붿껌??嫄곕??덉뒿?덈떎."),
			"?대?吏 ?댁긽?꾨? ??텛嫄곕굹 ???묒? ?뚯씪濡??ㅼ떆 ?쒕룄?섏꽭??",
			requestURL,
			statusCode,
			false,
		)
	case http.StatusUnsupportedMediaType:
		return newDoc2VLLMCallError(
			"unsupported_media_type",
			"Doc2VLLM OCR? ?꾩옱 ?낅젰 ?뺤떇??吏?먰븯吏 ?딆뒿?덈떎.",
			defaultIfEmpty(bodySummary, "吏?먮릺吏 ?딅뒗 ?낅젰 ?뺤떇?낅땲??"),
			"?대?吏 ?뚯씪(PNG, JPG, WEBP ?????ъ슜??二쇱꽭??",
			requestURL,
			statusCode,
			false,
		)
	default:
		if statusCode >= http.StatusInternalServerError {
			return newDoc2VLLMCallError(
				"server_error",
				"Doc2VLLM ?쒕쾭 ?대? ?ㅻ쪟媛 諛쒖깮?덉뒿?덈떎.",
				defaultIfEmpty(bodySummary, "Doc2VLLM ?쒕쾭媛 5xx ?ㅻ쪟瑜?諛섑솚?덉뒿?덈떎."),
				"?좎떆 ???ㅼ떆 ?쒕룄?섍퀬, 諛섎났?섎㈃ Doc2VLLM ?쒕쾭 濡쒓렇瑜??뺤씤?섏꽭??",
				requestURL,
				statusCode,
				true,
			)
		}
		return newDoc2VLLMCallError(
			"unexpected_status",
			fmt.Sprintf("Doc2VLLM???덉긽?섏? 紐삵븳 HTTP ?곹깭 %d 瑜?諛섑솚?덉뒿?덈떎.", statusCode),
			bodySummary,
			"?묐떟 蹂몃Ц怨?Doc2VLLM ?ㅼ젙???④퍡 ?뺤씤?섏꽭??",
			requestURL,
			statusCode,
			statusCode >= 500,
		)
	}
}

func classifyDoc2VLLMRequestError(requestURL string, err error) *doc2vllmCallError {
	detail := strings.TrimSpace(err.Error())

	var timeoutError interface{ Timeout() bool }
	if errors.As(err, &timeoutError) && timeoutError.Timeout() {
		return newDoc2VLLMCallError(
			"network_timeout",
			"The request to Doc2VLLM timed out.",
			detail,
			"Check the Doc2VLLM service status, network route, and plugin timeout settings.",
			requestURL,
			0,
			true,
		)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return newDoc2VLLMCallError(
			"network_timeout",
			"The request to Doc2VLLM timed out.",
			detail,
			"Check the Doc2VLLM service status and plugin timeout settings.",
			requestURL,
			0,
			true,
		)
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return newDoc2VLLMCallError(
			"dns_error",
			"Doc2VLLM ?몄뒪???대쫫??李얠? 紐삵뻽?듬땲??",
			detail,
			"湲곕낯 URL???꾨찓???대쫫怨?DNS ?ㅼ젙???뺤씤?섏꽭??",
			requestURL,
			0,
			false,
		)
	}

	var hostnameErr x509.HostnameError
	if errors.As(err, &hostnameErr) {
		return newDoc2VLLMCallError(
			"tls_hostname_error",
			"TLS ?몄쬆?쒖쓽 ?몄뒪???대쫫??Doc2VLLM URL怨??쇱튂?섏? ?딆뒿?덈떎.",
			detail,
			"?몄쬆?쒖쓽 SAN/CN怨?湲곕낯 URL ?몄뒪?멸? ?쇱튂?섎뒗吏 ?뺤씤?섏꽭??",
			requestURL,
			0,
			false,
		)
	}

	var unknownAuthorityErr x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthorityErr) {
		return newDoc2VLLMCallError(
			"tls_unknown_authority",
			"Doc2VLLM TLS ?몄쬆?쒕? ?좊ː?????놁뒿?덈떎.",
			detail,
			"?ъ꽕 ?몄쬆?쒕? ?ъ슜 以묒씠硫?Mattermost ?쒕쾭媛 ?대떦 猷⑦듃 ?몄쬆?쒕? ?좊ː?섎룄濡?援ъ꽦?섏꽭??",
			requestURL,
			0,
			false,
		)
	}

	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "connection refused"):
		return newDoc2VLLMCallError(
			"connection_refused",
			"Doc2VLLM ?쒕쾭媛 ?곌껐??嫄곕??덉뒿?덈떎.",
			detail,
			"Doc2VLLM API ?쒕쾭媛 ?ㅽ뻾 以묒씤吏, ?ы듃? 諛⑺솕踰쎌씠 ?щ컮瑜몄? ?뺤씤?섏꽭??",
			requestURL,
			0,
			true,
		)
	case strings.Contains(lower, "no such host"):
		return newDoc2VLLMCallError(
			"dns_error",
			"Doc2VLLM ?몄뒪???대쫫??李얠? 紐삵뻽?듬땲??",
			detail,
			"湲곕낯 URL???꾨찓???대쫫怨?DNS ?ㅼ젙???뺤씤?섏꽭??",
			requestURL,
			0,
			false,
		)
	case strings.Contains(lower, "certificate"), strings.Contains(lower, "tls"):
		return newDoc2VLLMCallError(
			"tls_error",
			"Doc2VLLM TLS ?곌껐???ㅼ젙?섏? 紐삵뻽?듬땲??",
			detail,
			"HTTPS ?몄쬆??泥댁씤怨??꾨줉??TLS 援ъ꽦???뺤씤?섏꽭??",
			requestURL,
			0,
			false,
		)
	default:
		return newDoc2VLLMCallError(
			"network_error",
			"Doc2VLLM ?쒕쾭???곌껐?섏? 紐삵뻽?듬땲??",
			detail,
			"湲곕낯 URL, ?ㅽ듃?뚰겕 寃쎈줈, 諛⑺솕踰? ?꾨줉???ㅼ젙???뺤씤?섏꽭??",
			requestURL,
			0,
			true,
		)
	}
}

func firstHeaderValue(headers http.Header, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(headers.Get(key)); value != "" {
			return value
		}
	}
	return ""
}
