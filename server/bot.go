package main

import (
	"fmt"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
)

const (
	defaultDoc2VLLMModel     = "Qwen/Qwen2.5-7B-Instruct"
	defaultDoc2VLLMMaxTokens = 1024
	defaultDoc2VLLMTopP      = 1.0
	defaultOutputMode        = "markdown"
)

type BotDefinition struct {
	ID                string          `json:"id"`
	Username          string          `json:"username"`
	DisplayName       string          `json:"display_name"`
	Description       string          `json:"description"`
	BaseURL           string          `json:"base_url,omitempty"`
	AuthMode          string          `json:"auth_mode,omitempty"`
	AuthToken         string          `json:"auth_token,omitempty"`
	Model             string          `json:"model,omitempty"`
	Mode              string          `json:"mode,omitempty"`
	OutputMode        string          `json:"output_mode,omitempty"`
	OCRPrompt         string          `json:"ocr_prompt,omitempty"`
	Temperature       float64         `json:"temperature,omitempty"`
	MaxTokens         int             `json:"max_tokens,omitempty"`
	TopP              float64         `json:"top_p,omitempty"`
	RepetitionPenalty float64         `json:"repetition_penalty,omitempty"`
	PresencePenalty   float64         `json:"presence_penalty,omitempty"`
	FrequencyPenalty  float64         `json:"frequency_penalty,omitempty"`
	ExtraRequestJSON  string          `json:"extra_request_json,omitempty"`
	MaskSensitiveData *bool           `json:"mask_sensitive_data,omitempty"`
	VLLMBaseURL       string          `json:"vllm_base_url,omitempty"`
	VLLMAPIKey        string          `json:"vllm_api_key,omitempty"`
	VLLMModel         string          `json:"vllm_model,omitempty"`
	VLLMPrompt        string          `json:"vllm_prompt,omitempty"`
	VLLMScope         string          `json:"vllm_scope,omitempty"`
	FlowID            string          `json:"flow_id,omitempty"`
	FileComponentID   string          `json:"file_component_id,omitempty"`
	ImageComponentID  string          `json:"image_component_id,omitempty"`
	AllowedTeams      []string        `json:"allowed_teams"`
	AllowedChannels   []string        `json:"allowed_channels"`
	AllowedUsers      []string        `json:"allowed_users"`
	InputSchema       []BotInputField `json:"input_schema,omitempty"`
}

type BotInputField struct {
	Name         string `json:"name"`
	Label        string `json:"label"`
	Description  string `json:"description"`
	Type         string `json:"type"`
	Required     bool   `json:"required"`
	Placeholder  string `json:"placeholder"`
	DefaultValue any    `json:"default_value"`
}

func (b BotDefinition) normalize() (BotDefinition, error) {
	b.ID = strings.TrimSpace(b.ID)
	b.Username = strings.ToLower(strings.TrimSpace(b.Username))
	b.DisplayName = strings.TrimSpace(b.DisplayName)
	b.Description = strings.TrimSpace(b.Description)
	b.BaseURL = strings.TrimSpace(b.BaseURL)
	b.AuthMode = strings.ToLower(strings.TrimSpace(b.AuthMode))
	if b.AuthMode != "" {
		b.AuthMode = normalizeAuthMode(b.AuthMode)
	}
	b.AuthToken = strings.TrimSpace(b.AuthToken)
	b.Model = defaultIfEmpty(strings.TrimSpace(b.Model), defaultDoc2VLLMModel)
	b.Mode = normalizeBotMode(b.Mode)
	b.OutputMode = normalizeOutputMode(b.OutputMode)
	b.OCRPrompt = strings.TrimSpace(b.OCRPrompt)
	b.Temperature = normalizeDoc2VLLMTemperature(b.Temperature)
	b.MaxTokens = positiveOrDefault(b.MaxTokens, defaultDoc2VLLMMaxTokens)
	b.TopP = normalizeDoc2VLLMTopP(b.TopP)
	b.RepetitionPenalty = normalizeRepetitionPenalty(b.RepetitionPenalty)
	b.PresencePenalty = normalizePenalty(b.PresencePenalty)
	b.FrequencyPenalty = normalizePenalty(b.FrequencyPenalty)
	b.ExtraRequestJSON = strings.TrimSpace(b.ExtraRequestJSON)
	b.VLLMBaseURL = strings.TrimSpace(b.VLLMBaseURL)
	b.VLLMAPIKey = strings.TrimSpace(b.VLLMAPIKey)
	b.VLLMModel = strings.TrimSpace(b.VLLMModel)
	b.VLLMPrompt = strings.TrimSpace(b.VLLMPrompt)
	b.VLLMScope = normalizeVLLMScope(b.VLLMScope)
	b.FlowID = strings.TrimSpace(b.FlowID)
	b.FileComponentID = strings.TrimSpace(b.FileComponentID)
	b.ImageComponentID = strings.TrimSpace(b.ImageComponentID)

	if b.Username == "" {
		return BotDefinition{}, fmt.Errorf("bot definition is missing username")
	}
	if b.ID == "" {
		b.ID = b.Username
	}
	if b.DisplayName == "" {
		b.DisplayName = b.Username
	}

	b.AllowedTeams = normalizeStringSlice(b.AllowedTeams)
	b.AllowedChannels = normalizeStringSlice(b.AllowedChannels)
	b.AllowedUsers = normalizeStringSlice(b.AllowedUsers)

	inputs := make([]BotInputField, 0, len(b.InputSchema))
	seen := map[string]struct{}{}
	for _, field := range b.InputSchema {
		field.Name = strings.TrimSpace(field.Name)
		field.Label = defaultIfEmpty(strings.TrimSpace(field.Label), field.Name)
		field.Description = strings.TrimSpace(field.Description)
		field.Placeholder = strings.TrimSpace(field.Placeholder)
		field.Type = defaultIfEmpty(strings.ToLower(strings.TrimSpace(field.Type)), "text")
		if field.Name == "" {
			return BotDefinition{}, fmt.Errorf("bot %q has an input field without a name", b.Username)
		}
		if _, ok := seen[field.Name]; ok {
			return BotDefinition{}, fmt.Errorf("bot %q defines duplicate input %q", b.Username, field.Name)
		}
		seen[field.Name] = struct{}{}
		inputs = append(inputs, field)
	}
	b.InputSchema = inputs

	return b, nil
}

func (b BotDefinition) effectiveDoc2VLLMPrompt(userPrompt string) string {
	if value := strings.TrimSpace(userPrompt); value != "" {
		return value
	}
	if value := strings.TrimSpace(b.OCRPrompt); value != "" {
		return value
	}
	if b.effectiveMode() == "multimodal" {
		return defaultDoc2VLLMMultimodalPrompt
	}
	return defaultDoc2VLLMOCRPrompt
}

func (b BotDefinition) effectiveOCRInstruction() string {
	if value := strings.TrimSpace(b.OCRPrompt); value != "" {
		return value
	}
	if b.effectiveMode() == "multimodal" {
		return defaultDoc2VLLMMultimodalPrompt
	}
	return defaultDoc2VLLMOCRPrompt
}

func (b BotDefinition) effectiveAttachmentUserPrompt(userPrompt string) string {
	if value := strings.TrimSpace(userPrompt); value != "" {
		return value
	}
	return defaultDoc2VLLMAttachmentUserPrompt
}

func (b BotDefinition) effectiveMode() string {
	return normalizeBotMode(b.Mode)
}

func (b BotDefinition) supportsVisionInputs() bool {
	return b.effectiveMode() != "chat"
}

func (b BotDefinition) effectiveDoc2VLLMTemperature() float64 {
	return normalizeDoc2VLLMTemperature(b.Temperature)
}

func (b BotDefinition) effectiveDoc2VLLMMaxTokens() int {
	return positiveOrDefault(b.MaxTokens, defaultDoc2VLLMMaxTokens)
}

func (b BotDefinition) effectiveDoc2VLLMTopP() float64 {
	return normalizeDoc2VLLMTopP(b.TopP)
}

func (b BotDefinition) effectiveOutputMode() string {
	return normalizeOutputMode(b.OutputMode)
}

func (b BotDefinition) shouldMaskSensitiveData(defaultValue bool) bool {
	if b.MaskSensitiveData != nil {
		return *b.MaskSensitiveData
	}
	return defaultValue
}

func (b BotDefinition) hasVLLMPostProcess() bool {
	return b.VLLMBaseURL != "" && b.VLLMModel != ""
}

func (b BotDefinition) effectiveVLLMScope() string {
	return normalizeVLLMScope(b.VLLMScope)
}

func (b BotDefinition) shouldUseVLLMForPostProcess() bool {
	if !b.hasVLLMPostProcess() {
		return false
	}

	switch b.effectiveVLLMScope() {
	case "both", "postprocess":
		return true
	default:
		return false
	}
}

func (b BotDefinition) shouldUseVLLMForFollowUps() bool {
	if !b.hasVLLMPostProcess() {
		return false
	}

	switch b.effectiveVLLMScope() {
	case "both", "followups":
		return true
	default:
		return false
	}
}

func (b BotDefinition) publicView() BotDefinition {
	copyBot := b
	copyBot.AuthToken = ""
	copyBot.VLLMAPIKey = ""
	return copyBot
}

func normalizeStringSlice(items []string) []string {
	normalized := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}
	return normalized
}

func normalizeDoc2VLLMTemperature(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 2 {
		return 2
	}
	return value
}

func normalizeDoc2VLLMTopP(value float64) float64 {
	if value <= 0 || value > 1 {
		return defaultDoc2VLLMTopP
	}
	return value
}

func normalizePenalty(value float64) float64 {
	if value < -2 {
		return -2
	}
	if value > 2 {
		return 2
	}
	return value
}

func normalizeRepetitionPenalty(value float64) float64 {
	if value == 0 {
		return 1
	}
	if value < 0.1 {
		return 0.1
	}
	if value > 2 {
		return 2
	}
	return value
}

func normalizeBotMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "chat", "text", "text-generation", "generation":
		return "chat"
	case "multimodal", "vision", "vlm":
		return "multimodal"
	default:
		return "ocr"
	}
}

func normalizeOutputMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "markdown":
		return defaultOutputMode
	case "text", "json":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return defaultOutputMode
	}
}

func normalizeVLLMScope(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "postprocess":
		return "postprocess"
	case "followups", "both":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "postprocess"
	}
}

func boolPtr(value bool) *bool {
	item := value
	return &item
}

func (cfg *runtimeConfiguration) getBotByID(botID string) *BotDefinition {
	botID = strings.ToLower(strings.TrimSpace(botID))
	for _, bot := range cfg.BotDefinitions {
		if strings.ToLower(bot.ID) == botID || strings.ToLower(bot.Username) == botID {
			item := bot
			return &item
		}
	}
	return nil
}

func (cfg *runtimeConfiguration) getAllowedBots(user *model.User, channel *model.Channel, team *model.Team) []BotDefinition {
	allowed := make([]BotDefinition, 0, len(cfg.BotDefinitions))
	for _, bot := range cfg.BotDefinitions {
		if bot.isAllowedFor(user, channel, team) {
			allowed = append(allowed, bot)
		}
	}
	return allowed
}

func (b BotDefinition) isAllowedFor(user *model.User, channel *model.Channel, team *model.Team) bool {
	if user == nil || channel == nil {
		return false
	}

	if len(b.AllowedUsers) > 0 && !matchesAccessEntry(b.AllowedUsers, user.Id, user.Username) {
		return false
	}
	if len(b.AllowedChannels) > 0 && !matchesAccessEntry(b.AllowedChannels, channel.Id, channel.Name) {
		return false
	}

	teamName := ""
	if team != nil {
		teamName = team.Name
	}
	if len(b.AllowedTeams) > 0 && !matchesAccessEntry(b.AllowedTeams, channel.TeamId, teamName) {
		return false
	}

	return true
}

func matchesAccessEntry(entries []string, values ...string) bool {
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		for _, entry := range entries {
			if entry == value {
				return true
			}
		}
	}
	return false
}

func genericBotUsageExamples(bot BotDefinition) []string {
	examples := []string{
		fmt.Sprintf("- `@%s 오늘 회의 내용을 5줄로 정리해줘`", bot.Username),
		fmt.Sprintf("- `@%s 이 문서를 요약하고 액션 아이템만 뽑아줘` + PDF/DOCX/XLSX/PPTX 첨부", bot.Username),
	}
	if bot.supportsVisionInputs() {
		examples = append(examples, fmt.Sprintf("- `@%s 첨부 이미지의 핵심 내용을 설명해줘` + 이미지 첨부", bot.Username))
	}
	examples = append(examples, fmt.Sprintf("- DM `%s` 로 텍스트만 보내거나 파일을 같이 첨부해서 사용", bot.Username))
	return examples
}

func botUsageExamples(bot BotDefinition) []string {
	return []string{
		fmt.Sprintf("- `@%s 이미지에서 텍스트 추출해줘` 와 함께 파일 첨부", bot.Username),
		fmt.Sprintf("- `@%s 표만 읽어서 정리해줘` 와 함께 이미지, PDF, DOCX, XLSX 업로드", bot.Username),
		fmt.Sprintf("- `@%s 발표 자료 핵심 문구만 모아줘` 와 함께 PPTX 업로드", bot.Username),
		fmt.Sprintf("- DM `%s` 에 이미지/PDF/DOCX/XLSX/PPTX 파일을 붙여 전송", bot.Username),
	}
}
