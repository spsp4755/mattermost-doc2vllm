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
)

const (
	defaultDoc2VLLMOCRPrompt = "이미지에서 텍스트를 추출해 주세요."
)

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
	ErrorCode  string `json:"error_code,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Hint       string `json:"hint,omitempty"`
	Retryable  bool   `json:"retryable"`
}

type doc2vllmChatRequest struct {
	Model       string            `json:"model"`
	Messages    []doc2vllmMessage `json:"messages"`
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
	Prompt              string                  `json:"prompt"`
	SystemPrompt        string                  `json:"system_prompt,omitempty"`
	UserPrompt          string                  `json:"user_prompt,omitempty"`
	EffectiveUserPrompt string                  `json:"effective_user_prompt,omitempty"`
	Temperature         float64                 `json:"temperature"`
	MaxTokens           int                     `json:"max_tokens"`
	TopP                float64                 `json:"top_p"`
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
		lines = append(lines, "상세: "+e.Detail)
	}
	if e.Hint != "" {
		lines = append(lines, "조치: "+e.Hint)
	}
	if e.StatusCode > 0 {
		lines = append(lines, fmt.Sprintf("HTTP 상태: %d", e.StatusCode))
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
	effectiveUserPrompt := rawUserPrompt
	if effectiveUserPrompt == "" {
		effectiveUserPrompt = bot.effectiveOCRInstruction()
	}

	messages := make([]doc2vllmMessage, 0, 2)
	systemPrompt := buildDoc2VLLMSystemPrompt(bot, documentContext, attachment != nil)
	if systemPrompt != "" {
		messages = append(messages, doc2vllmMessage{
			Role:    "system",
			Content: systemPrompt,
		})
	}

	userContent := []doc2vllmContentPart{{
		Type: "text",
		Text: buildDoc2VLLMUserPrompt(rawUserPrompt, effectiveUserPrompt, documentContext, turns, attachment != nil),
	}}
	debugAttachment := botAttachment{}
	userMessage := doc2vllmMessage{
		Role:    "user",
		Content: userContent[0].Text,
	}
	if attachment != nil {
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
	}

	requestPayload := doc2vllmChatRequest{
		Model:       defaultIfEmpty(strings.TrimSpace(bot.Model), defaultDoc2VLLMModel),
		Messages:    append(messages, userMessage),
		Temperature: bot.effectiveDoc2VLLMTemperature(),
		MaxTokens:   bot.effectiveDoc2VLLMMaxTokens(),
		TopP:        bot.effectiveDoc2VLLMTopP(),
	}

	return requestPayload, buildDoc2VLLMRequestDebug(service, requestPayload, debugAttachment, systemPrompt, rawUserPrompt, effectiveUserPrompt, correlationID), effectiveUserPrompt, nil
}

func buildDoc2VLLMSystemPrompt(bot BotDefinition, documentContext string, hasAttachment bool) string {
	parts := []string{
		"You are an OCR document assistant.",
	}
	if instruction := strings.TrimSpace(bot.effectiveOCRInstruction()); instruction != "" {
		parts = append(parts, instruction)
	}
	if hasAttachment {
		parts = append(parts, "Read the attached document carefully and answer in the same language as the user when possible.")
	} else if strings.TrimSpace(documentContext) != "" {
		parts = append(parts, "Use the previously extracted document context as the source of truth for follow-up answers. If the answer is not grounded in the document context, say so clearly.")
		parts = append(parts, "Answer only the current user request. Do not repeat the full OCR transcript or the full document unless the user explicitly asks for it.")
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func buildDoc2VLLMUserPrompt(userPrompt, effectiveUserPrompt, documentContext string, turns []conversationTurn, hasAttachment bool) string {
	userPrompt = strings.TrimSpace(userPrompt)
	effectiveUserPrompt = strings.TrimSpace(effectiveUserPrompt)
	if hasAttachment {
		if effectiveUserPrompt == "" {
			return "Process the attached document."
		}
		return effectiveUserPrompt
	}

	parts := make([]string, 0, 4)
	if strings.TrimSpace(documentContext) != "" {
		parts = append(parts, "[Document context]\n"+strings.TrimSpace(documentContext))
	}
	if history := buildDoc2VLLMConversationHistory(turns); history != "" {
		parts = append(parts, "[Conversation history]\n"+history)
	}
	currentRequest := userPrompt
	if currentRequest == "" {
		currentRequest = effectiveUserPrompt
	}
	parts = append(parts, "[Current user request]\n"+defaultIfEmpty(strings.TrimSpace(currentRequest), "Please answer using the extracted document context."))
	if !hasAttachment {
		parts = append(parts, "[Answering rules]\nRespond to the current request only. Do not repeat the full OCR result unless explicitly requested.")
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func buildDoc2VLLMConversationHistory(turns []conversationTurn) string {
	normalized := normalizeConversationTurns(turns)
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
			"Doc2VLLM OCR은 이미지 첨부만 지원합니다.",
			fmt.Sprintf("현재 파일 형식: %s", defaultIfEmpty(mimeType, "unknown")),
			"PNG, JPG, WEBP 같은 이미지 파일을 첨부해 주세요.",
			"",
			0,
			false,
		)
	}
	if len(attachment.Content) == 0 {
		return "", newDoc2VLLMCallError(
			"empty_attachment",
			"빈 이미지 파일은 처리할 수 없습니다.",
			sanitizeUploadFilename(attachment.Name),
			"이미지 파일이 정상적으로 업로드되었는지 확인하세요.",
			"",
			0,
			false,
		)
	}

	encoded := base64.StdEncoding.EncodeToString(attachment.Content)
	return fmt.Sprintf("data:%s;base64,%s", mimeType, encoded), nil
}

func (p *Plugin) performDoc2VLLMRequest(
	ctx context.Context,
	service doc2vllmServiceConfig,
	bot BotDefinition,
	attachment botAttachment,
	requestPayload doc2vllmChatRequest,
	requestDebug doc2vllmRequestDebug,
) (doc2vllmDocumentResult, int, error) {
	bodyBytes, err := json.Marshal(requestPayload)
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
			"Doc2VLLM 응답 본문을 읽는 중 오류가 발생했습니다.",
			err.Error(),
			"Doc2VLLM 서버 상태와 응답 크기 제한을 확인하세요.",
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
			"Doc2VLLM 응답 JSON을 해석하지 못했습니다.",
			err.Error(),
			"Doc2VLLM OpenAI 호환 엔드포인트와 응답 형식을 확인하세요.",
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

func (p *Plugin) testDoc2VLLMConnection(ctx context.Context, cfg *runtimeConfiguration) (*doc2vllmConnectionStatus, error) {
	serviceConfig, err := cfg.serviceConfigForBot(BotDefinition{})
	if err != nil {
		return nil, err
	}

	requestPayload := doc2vllmChatRequest{
		Model: defaultDoc2VLLMModel,
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
			Message:    "엔드포인트 연결과 인증은 확인되었습니다. 테스트 요청은 OCR 입력 이미지가 없어 예상대로 거부되었습니다.",
		}, nil
	}
	if response.StatusCode >= http.StatusBadRequest {
		return classifyDoc2VLLMHTTPError(serviceConfig.BaseURL, response.StatusCode, response.Header, bodyBytes).toConnectionStatus(), nil
	}

	return &doc2vllmConnectionStatus{
		OK:         true,
		URL:        serviceConfig.BaseURL,
		StatusCode: response.StatusCode,
		Message:    defaultIfEmpty(strings.TrimSpace(extractTextFromBody(bodyBytes)), "연결에 성공했습니다."),
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
	value = strings.TrimSpace(value)
	if maxLength <= 0 || len(value) <= maxLength {
		return value
	}
	if maxLength <= 3 {
		return value[:maxLength]
	}
	return value[:maxLength-3] + "..."
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
	requestPayload doc2vllmChatRequest,
	attachment botAttachment,
	systemPrompt string,
	userPrompt string,
	effectiveUserPrompt string,
	correlationID string,
) doc2vllmRequestDebug {
	return doc2vllmRequestDebug{
		URL:                 strings.TrimSpace(service.BaseURL),
		AuthMode:            strings.TrimSpace(service.AuthMode),
		Model:               strings.TrimSpace(requestPayload.Model),
		Prompt:              truncateString(strings.TrimSpace(effectiveUserPrompt), 2000),
		SystemPrompt:        truncateString(strings.TrimSpace(systemPrompt), 2000),
		UserPrompt:          truncateString(strings.TrimSpace(userPrompt), 2000),
		EffectiveUserPrompt: truncateString(strings.TrimSpace(effectiveUserPrompt), 2000),
		Temperature:         requestPayload.Temperature,
		MaxTokens:           requestPayload.MaxTokens,
		TopP:                requestPayload.TopP,
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
	return string(raw)
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
			"Doc2VLLM OCR 요청이 거부되었습니다.",
			defaultIfEmpty(bodySummary, "messages 또는 image_url 형식이 Doc2VLLM 요구사항과 맞지 않습니다."),
			"model, messages, image_url.url, max_tokens 값을 확인하세요.",
			requestURL,
			statusCode,
			false,
		)
	case http.StatusUnauthorized, http.StatusForbidden:
		return newDoc2VLLMCallError(
			"auth_failed",
			"Doc2VLLM 인증에 실패했습니다.",
			defaultIfEmpty(bodySummary, "API 키가 유효하지 않거나 권한이 없습니다."),
			"System Console의 인증 토큰과 헤더 방식을 확인하세요.",
			requestURL,
			statusCode,
			false,
		)
	case http.StatusNotFound:
		return newDoc2VLLMCallError(
			"not_found",
			"Doc2VLLM API 엔드포인트를 찾지 못했습니다.",
			defaultIfEmpty(bodySummary, "chat/completions 경로가 올바르지 않습니다."),
			"기본 URL이 OpenAI 호환 chat completions 엔드포인트를 가리키는지 확인하세요.",
			requestURL,
			statusCode,
			false,
		)
	case http.StatusTooManyRequests:
		return newDoc2VLLMCallError(
			"rate_limited",
			"Doc2VLLM 호출 한도에 걸렸습니다.",
			defaultIfEmpty(bodySummary, "잠시 후 다시 시도해야 합니다."),
			"요청 빈도를 줄이거나 잠시 후 다시 시도하세요.",
			requestURL,
			statusCode,
			true,
		)
	case http.StatusRequestEntityTooLarge:
		return newDoc2VLLMCallError(
			"image_too_large",
			"업로드한 이미지가 너무 큽니다.",
			defaultIfEmpty(bodySummary, "Doc2VLLM이 이미지 크기 제한을 초과한 요청을 거부했습니다."),
			"이미지 해상도를 낮추거나 더 작은 파일로 다시 시도하세요.",
			requestURL,
			statusCode,
			false,
		)
	case http.StatusUnsupportedMediaType:
		return newDoc2VLLMCallError(
			"unsupported_media_type",
			"Doc2VLLM OCR은 현재 입력 형식을 지원하지 않습니다.",
			defaultIfEmpty(bodySummary, "지원되지 않는 입력 형식입니다."),
			"이미지 파일(PNG, JPG, WEBP 등)을 사용해 주세요.",
			requestURL,
			statusCode,
			false,
		)
	default:
		if statusCode >= http.StatusInternalServerError {
			return newDoc2VLLMCallError(
				"server_error",
				"Doc2VLLM 서버 내부 오류가 발생했습니다.",
				defaultIfEmpty(bodySummary, "Doc2VLLM 서버가 5xx 오류를 반환했습니다."),
				"잠시 후 다시 시도하고, 반복되면 Doc2VLLM 서버 로그를 확인하세요.",
				requestURL,
				statusCode,
				true,
			)
		}
		return newDoc2VLLMCallError(
			"unexpected_status",
			fmt.Sprintf("Doc2VLLM이 예상하지 못한 HTTP 상태 %d 를 반환했습니다.", statusCode),
			bodySummary,
			"응답 본문과 Doc2VLLM 설정을 함께 확인하세요.",
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
			"Doc2VLLM 서버 연결이 시간 초과되었습니다.",
			detail,
			"Doc2VLLM 서버 상태와 네트워크 지연, 플러그인 타임아웃 설정을 확인하세요.",
			requestURL,
			0,
			true,
		)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return newDoc2VLLMCallError(
			"network_timeout",
			"Doc2VLLM 서버 연결이 시간 초과되었습니다.",
			detail,
			"Doc2VLLM 서버 상태와 플러그인 타임아웃 값을 확인하세요.",
			requestURL,
			0,
			true,
		)
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return newDoc2VLLMCallError(
			"dns_error",
			"Doc2VLLM 호스트 이름을 찾지 못했습니다.",
			detail,
			"기본 URL의 도메인 이름과 DNS 설정을 확인하세요.",
			requestURL,
			0,
			false,
		)
	}

	var hostnameErr x509.HostnameError
	if errors.As(err, &hostnameErr) {
		return newDoc2VLLMCallError(
			"tls_hostname_error",
			"TLS 인증서의 호스트 이름이 Doc2VLLM URL과 일치하지 않습니다.",
			detail,
			"인증서의 SAN/CN과 기본 URL 호스트가 일치하는지 확인하세요.",
			requestURL,
			0,
			false,
		)
	}

	var unknownAuthorityErr x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthorityErr) {
		return newDoc2VLLMCallError(
			"tls_unknown_authority",
			"Doc2VLLM TLS 인증서를 신뢰할 수 없습니다.",
			detail,
			"사설 인증서를 사용 중이면 Mattermost 서버가 해당 루트 인증서를 신뢰하도록 구성하세요.",
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
			"Doc2VLLM 서버가 연결을 거부했습니다.",
			detail,
			"Doc2VLLM API 서버가 실행 중인지, 포트와 방화벽이 올바른지 확인하세요.",
			requestURL,
			0,
			true,
		)
	case strings.Contains(lower, "no such host"):
		return newDoc2VLLMCallError(
			"dns_error",
			"Doc2VLLM 호스트 이름을 찾지 못했습니다.",
			detail,
			"기본 URL의 도메인 이름과 DNS 설정을 확인하세요.",
			requestURL,
			0,
			false,
		)
	case strings.Contains(lower, "certificate"), strings.Contains(lower, "tls"):
		return newDoc2VLLMCallError(
			"tls_error",
			"Doc2VLLM TLS 연결을 설정하지 못했습니다.",
			detail,
			"HTTPS 인증서 체인과 프록시 TLS 구성을 확인하세요.",
			requestURL,
			0,
			false,
		)
	default:
		return newDoc2VLLMCallError(
			"network_error",
			"Doc2VLLM 서버에 연결하지 못했습니다.",
			detail,
			"기본 URL, 네트워크 경로, 방화벽, 프록시 설정을 확인하세요.",
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
