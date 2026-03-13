package main

import (
	"archive/zip"
	"bytes"
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/require"
)

func TestParseBotDefinitions(t *testing.T) {
	bots, err := parseBotDefinitions(`[
		{"id":"ocr-bot","username":"ocr-bot","display_name":"OCR Bot","model":"doc2vllm-ocr","ocr_prompt":"텍스트를 추출해 주세요.","temperature":0.2,"max_tokens":2048,"top_p":0.8},
		{"id":"table-bot","username":"table-bot","display_name":"Table Bot","max_tokens":4096}
	]`)
	require.NoError(t, err)
	require.Len(t, bots, 2)
	require.Equal(t, "ocr-bot", bots[0].ID)
	require.Equal(t, "텍스트를 추출해 주세요.", bots[0].OCRPrompt)
	require.Equal(t, 0.2, bots[0].Temperature)
	require.Equal(t, 2048, bots[0].MaxTokens)
	require.Equal(t, 0.8, bots[0].TopP)
	require.Equal(t, "table-bot", bots[1].Username)
	require.Equal(t, 4096, bots[1].MaxTokens)
}

func TestParseBotDefinitionsAutoAssignsIDFromUsername(t *testing.T) {
	bots, err := parseBotDefinitions(`[{"username":"summary-bot","display_name":"Thread Summary"}]`)
	require.NoError(t, err)
	require.Len(t, bots, 1)
	require.Equal(t, "summary-bot", bots[0].ID)
	require.Equal(t, defaultDoc2VLLMModel, bots[0].Model)
	require.Equal(t, defaultOutputMode, bots[0].OutputMode)
	require.Equal(t, defaultDoc2VLLMMaxTokens, bots[0].MaxTokens)
	require.Equal(t, defaultDoc2VLLMTopP, bots[0].TopP)
}

func TestParseBotDefinitionsPreservesEmptyBotAuthMode(t *testing.T) {
	bots, err := parseBotDefinitions(`[{"username":"summary-bot","display_name":"Thread Summary","auth_mode":""}]`)
	require.NoError(t, err)
	require.Len(t, bots, 1)
	require.Equal(t, "", bots[0].AuthMode)
}

func TestParseBotDefinitionsIncludesVLLMAndMasking(t *testing.T) {
	bots, err := parseBotDefinitions(`[{"username":"summary-bot","display_name":"Thread Summary","mask_sensitive_data":true,"vllm_base_url":"http://localhost:8000/v1","vllm_model":"Qwen/Qwen2.5-7B-Instruct","vllm_prompt":"{{document_text}}"}]`)
	require.NoError(t, err)
	require.Len(t, bots, 1)
	require.True(t, bots[0].shouldMaskSensitiveData(false))
	require.True(t, bots[0].hasVLLMPostProcess())
	require.Equal(t, "http://localhost:8000/v1", bots[0].VLLMBaseURL)
	require.Equal(t, "Qwen/Qwen2.5-7B-Instruct", bots[0].VLLMModel)
}

func TestConfigurationGetStoredPluginConfigDefaultsWhenEmpty(t *testing.T) {
	cfg := &configuration{}
	stored, source, err := cfg.getStoredPluginConfig()
	require.NoError(t, err)
	require.Equal(t, "config", source)
	require.Equal(t, defaultDoc2VLLMEndpointURL, stored.Service.BaseURL)
	require.Equal(t, "", stored.Service.AuthToken)
	require.Equal(t, defaultTimeoutSeconds, stored.Runtime.DefaultTimeoutSeconds)
	require.Equal(t, defaultPDFRasterDPI, stored.Runtime.PDFRasterDPI)
	require.Equal(t, defaultMaxPDFPages, stored.Runtime.MaxPDFPages)
	require.True(t, stored.Runtime.EnableUsageLogs)
	require.Empty(t, stored.Bots)
}

func TestConfigurationNormalizeFromConfig(t *testing.T) {
	cfg := &configuration{
		Config: `{
			"service": {
				"base_url": "http://localhost:8000/v1",
				"auth_mode": "x-api-key",
				"auth_token": "secret"
			},
			"runtime": {
				"default_timeout_seconds": 55,
				"max_input_length": 5000,
				"max_output_length": 9000,
				"pdf_raster_dpi": 300,
				"max_pdf_pages": 12,
				"mask_sensitive_data": true,
				"enable_debug_logs": true,
				"enable_usage_logs": false
			},
			"bots": [
				{"username":"summary-bot","display_name":"Thread Summary","model":"doc2vllm-ocr","max_tokens":1536}
			]
		}`,
	}

	runtimeCfg, err := cfg.normalize()
	require.NoError(t, err)
	require.Equal(t, "http://localhost:8000/v1/chat/completions", runtimeCfg.ServiceBaseURL)
	require.Equal(t, "x-api-key", runtimeCfg.AuthMode)
	require.Equal(t, "secret", runtimeCfg.AuthToken)
	require.Equal(t, 55, int(runtimeCfg.DefaultTimeout.Seconds()))
	require.Equal(t, 300, runtimeCfg.PDFRasterDPI)
	require.Equal(t, 12, runtimeCfg.MaxPDFPages)
	require.True(t, runtimeCfg.MaskSensitiveData)
	require.False(t, runtimeCfg.EnableUsageLogs)
	require.Len(t, runtimeCfg.BotDefinitions, 1)
	require.Equal(t, "summary-bot", runtimeCfg.BotDefinitions[0].ID)
}

func TestNormalizeDoc2VLLMEndpointURL(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "root path",
			input:    "http://localhost:8000",
			expected: "http://localhost:8000/v1/chat/completions",
		},
		{
			name:     "v1 root",
			input:    "http://localhost:8000/v1",
			expected: "http://localhost:8000/v1/chat/completions",
		},
		{
			name:     "existing endpoint",
			input:    "http://localhost:8000/v1/chat/completions",
			expected: "http://localhost:8000/v1/chat/completions",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			normalized, _, err := normalizeDoc2VLLMEndpointURL(testCase.input)
			require.NoError(t, err)
			require.Equal(t, testCase.expected, normalized)
		})
	}
}

func TestNormalizeVLLMEndpointURL(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "root path",
			input:    "http://localhost:8000",
			expected: "http://localhost:8000/v1/chat/completions",
		},
		{
			name:     "v1 root",
			input:    "http://localhost:8000/v1",
			expected: "http://localhost:8000/v1/chat/completions",
		},
		{
			name:     "chat completions",
			input:    "http://localhost:8000/v1/chat/completions",
			expected: "http://localhost:8000/v1/chat/completions",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			normalized, _, err := normalizeVLLMEndpointURL(testCase.input)
			require.NoError(t, err)
			require.Equal(t, testCase.expected, normalized)
		})
	}
}

func TestBuildDoc2VLLMChatRequest(t *testing.T) {
	bot, err := (BotDefinition{
		Username:    "ocr-bot",
		Model:       "doc2vllm-ocr",
		OCRPrompt:   "이미지를 읽어 주세요.",
		Temperature: 0.3,
		MaxTokens:   2048,
		TopP:        0.9,
	}).normalize()
	require.NoError(t, err)

	requestPayload, requestDebug, requestPrompt, err := buildDoc2VLLMChatRequest(
		doc2vllmServiceConfig{BaseURL: "http://localhost:8000/v1/chat/completions", AuthMode: "bearer"},
		bot,
		botAttachment{Name: "sample.png", MIMEType: "image/png", Content: []byte("png")},
		"",
		"corr-123",
	)
	require.NoError(t, err)
	require.Equal(t, "이미지를 읽어 주세요.", requestPrompt)
	require.Equal(t, "doc2vllm-ocr", requestPayload.Model)
	require.Equal(t, 0.3, requestPayload.Temperature)
	require.Equal(t, 2048, requestPayload.MaxTokens)
	require.Equal(t, 0.9, requestPayload.TopP)
	require.Len(t, requestPayload.Messages, 1)
	require.Len(t, requestPayload.Messages[0].Content, 2)
	require.Equal(t, "text", requestPayload.Messages[0].Content[0].Type)
	require.Equal(t, "image_url", requestPayload.Messages[0].Content[1].Type)
	require.True(t, strings.HasPrefix(requestPayload.Messages[0].Content[1].ImageURL.URL, "data:image/png;base64,"))
	require.Equal(t, "sample.png", requestDebug.Attachment.Name)
}

func TestBuildDoc2VLLMImageDataURLRejectsNonImages(t *testing.T) {
	_, err := buildDoc2VLLMImageDataURL(botAttachment{Name: "sample.pdf", MIMEType: "application/pdf", Content: []byte("pdf")})
	require.Error(t, err)
	require.Contains(t, err.Error(), "이미지 첨부만 지원")
}

func TestPrepareOCRInputsKeepsImages(t *testing.T) {
	plugin := &Plugin{}
	attachments := []botAttachment{{
		Name:      "invoice.png",
		MIMEType:  "image/png",
		Extension: "png",
		Content:   []byte("png"),
	}}

	prepared, failures := plugin.prepareOCRInputs(context.Background(), &runtimeConfiguration{}, attachments)
	require.Empty(t, failures)
	require.Len(t, prepared, 1)
	require.Equal(t, attachments[0].Name, prepared[0].Attachment.Name)
}

func TestPrepareOCRInputsUsesSearchablePDFTextWhenAvailable(t *testing.T) {
	plugin := &Plugin{}

	originalExtract := extractPDFAttachmentText
	originalConvert := convertPDFAttachmentToImages
	extractPDFAttachmentText = func(ctx context.Context, attachment botAttachment, options pdfProcessingOptions) (pdfTextExtraction, error) {
		require.Equal(t, "report.pdf", attachment.Name)
		require.Equal(t, 10, options.MaxPages)
		return pdfTextExtraction{Text: "첫 번째 페이지 텍스트", Processor: "pdftotext"}, nil
	}
	convertPDFAttachmentToImages = func(ctx context.Context, attachment botAttachment, options pdfProcessingOptions) ([]botAttachment, error) {
		t.Fatalf("rasterization should not run when searchable PDF text is available")
		return nil, nil
	}
	defer func() {
		extractPDFAttachmentText = originalExtract
		convertPDFAttachmentToImages = originalConvert
	}()

	prepared, failures := plugin.prepareOCRInputs(context.Background(), &runtimeConfiguration{MaxPDFPages: 10}, []botAttachment{{
		Name:      "report.pdf",
		MIMEType:  "application/pdf",
		Extension: "pdf",
		Content:   []byte("%PDF"),
	}})
	require.Empty(t, failures)
	require.Len(t, prepared, 1)
	require.NotNil(t, prepared[0].DirectResult)
	require.Equal(t, "pdf_text", prepared[0].DirectResult.Source)
	require.Equal(t, "pdftotext", prepared[0].DirectResult.Processor)
}

func TestPrepareOCRInputsConvertsPDFsToImagesWhenTextIsUnavailable(t *testing.T) {
	plugin := &Plugin{}

	originalExtract := extractPDFAttachmentText
	originalConvert := convertPDFAttachmentToImages
	extractPDFAttachmentText = func(ctx context.Context, attachment botAttachment, options pdfProcessingOptions) (pdfTextExtraction, error) {
		return pdfTextExtraction{}, nil
	}
	convertPDFAttachmentToImages = func(ctx context.Context, attachment botAttachment, options pdfProcessingOptions) ([]botAttachment, error) {
		require.Equal(t, 144, options.RasterDPI)
		require.Equal(t, 3, options.MaxPages)
		return []botAttachment{
			{Name: "report.pdf (page 1)", MIMEType: "image/png", Extension: "png", Content: []byte("page-1")},
			{Name: "report.pdf (page 2)", MIMEType: "image/png", Extension: "png", Content: []byte("page-2")},
		}, nil
	}
	defer func() {
		extractPDFAttachmentText = originalExtract
		convertPDFAttachmentToImages = originalConvert
	}()

	prepared, failures := plugin.prepareOCRInputs(context.Background(), &runtimeConfiguration{PDFRasterDPI: 144, MaxPDFPages: 3}, []botAttachment{{
		Name:      "report.pdf",
		MIMEType:  "application/pdf",
		Extension: "pdf",
		Content:   []byte("%PDF"),
	}})
	require.Empty(t, failures)
	require.Len(t, prepared, 2)
	require.Equal(t, "report.pdf (page 1)", prepared[0].Attachment.Name)
	require.Equal(t, "report.pdf (page 2)", prepared[1].Attachment.Name)
}

func TestPrepareOCRInputsCollectsUnsupportedFileFailures(t *testing.T) {
	plugin := &Plugin{}

	prepared, failures := plugin.prepareOCRInputs(context.Background(), &runtimeConfiguration{}, []botAttachment{{
		Name:      "legacy.xls",
		MIMEType:  "application/vnd.ms-excel",
		Extension: "xls",
		Content:   []byte("xls"),
	}})
	require.Empty(t, prepared)
	require.Len(t, failures, 1)
	require.Equal(t, "unsupported_media_type", failures[0].ErrorCode)
	require.Contains(t, failures[0].Message, "이미지, PDF, DOCX, XLSX, PPTX 첨부를 지원")
}

func TestPrepareOCRInputsExtractsDOCXText(t *testing.T) {
	plugin := &Plugin{}

	content := buildTestDOCX(t, map[string]string{
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>첫 번째 문단</w:t></w:r></w:p><w:tbl><w:tr><w:tc><w:p><w:r><w:t>A1</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>B1</w:t></w:r></w:p></w:tc></w:tr></w:tbl></w:body></w:document>`,
	})

	prepared, failures := plugin.prepareOCRInputs(context.Background(), &runtimeConfiguration{}, []botAttachment{{
		Name:      "report.docx",
		MIMEType:  "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		Extension: "docx",
		Content:   content,
	}})
	require.Empty(t, failures)
	require.Len(t, prepared, 1)
	require.NotNil(t, prepared[0].DirectResult)
	require.Equal(t, "docx_text", prepared[0].DirectResult.Source)
	require.Equal(t, "zip+xml", prepared[0].DirectResult.Processor)
	require.Contains(t, extractDoc2VLLMResponseText(prepared[0].DirectResult.Response), "첫 번째 문단")
	require.Contains(t, extractDoc2VLLMResponseText(prepared[0].DirectResult.Response), "A1")
}

func TestPrepareOCRInputsExtractsXLSXText(t *testing.T) {
	plugin := &Plugin{}

	content := buildTestOOXMLArchive(t, map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Sheet1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/sharedStrings.xml":       `<sst><si><t>Hello</t></si><si><t>World</t></si></sst>`,
		"xl/worksheets/sheet1.xml":   `<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row><row r="2"><c r="A2"><v>42</v></c><c r="B2" t="inlineStr"><is><t>Inline</t></is></c></row></sheetData></worksheet>`,
	})

	prepared, failures := plugin.prepareOCRInputs(context.Background(), &runtimeConfiguration{}, []botAttachment{{
		Name:      "sheet.xlsx",
		MIMEType:  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		Extension: "xlsx",
		Content:   content,
	}})
	require.Empty(t, failures)
	require.Len(t, prepared, 1)
	require.NotNil(t, prepared[0].DirectResult)
	require.Equal(t, "xlsx_text", prepared[0].DirectResult.Source)
	require.Contains(t, extractDoc2VLLMResponseText(prepared[0].DirectResult.Response), "## Sheet: Sheet1")
	require.Contains(t, extractDoc2VLLMResponseText(prepared[0].DirectResult.Response), "Hello\tWorld")
	require.Contains(t, extractDoc2VLLMResponseText(prepared[0].DirectResult.Response), "42\tInline")
}

func TestPrepareOCRInputsExtractsPPTXText(t *testing.T) {
	plugin := &Plugin{}

	content := buildTestOOXMLArchive(t, map[string]string{
		"ppt/slides/slide1.xml": `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>Title</a:t></a:r></a:p><a:p><a:r><a:t>Bullet 1</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
		"ppt/slides/slide2.xml": `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>Second Slide</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
	})

	prepared, failures := plugin.prepareOCRInputs(context.Background(), &runtimeConfiguration{}, []botAttachment{{
		Name:      "deck.pptx",
		MIMEType:  "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		Extension: "pptx",
		Content:   content,
	}})
	require.Empty(t, failures)
	require.Len(t, prepared, 1)
	require.NotNil(t, prepared[0].DirectResult)
	require.Equal(t, "pptx_text", prepared[0].DirectResult.Source)
	require.Contains(t, extractDoc2VLLMResponseText(prepared[0].DirectResult.Response), "## Slide 1")
	require.Contains(t, extractDoc2VLLMResponseText(prepared[0].DirectResult.Response), "Bullet 1")
	require.Contains(t, extractDoc2VLLMResponseText(prepared[0].DirectResult.Response), "## Slide 2")
}

func TestBuildRenderableDoc2VLLMContent(t *testing.T) {
	format, content := buildRenderableDoc2VLLMContent(doc2vllmOCRResponse{
		Model: "doc2vllm-ocr",
		Choices: []doc2vllmChoice{{
			Message: doc2vllmChoiceMessage{
				Role:    "assistant",
				Content: "Invoice total: 12000",
			},
		}},
	})

	require.Equal(t, "text", format)
	require.Equal(t, "Invoice total: 12000", content)
}

func TestBuildDocumentResponseMarkdownIncludesUsage(t *testing.T) {
	message := buildDocumentResponseMarkdown("", []doc2vllmDocumentResult{{
		Attachment:    botAttachment{Name: "invoice.png"},
		RequestPrompt: "이미지에서 텍스트를 추출해 주세요.",
		Response: doc2vllmOCRResponse{
			Model: "doc2vllm-ocr",
			Choices: []doc2vllmChoice{{
				Message: doc2vllmChoiceMessage{
					Role:    "assistant",
					Content: "총액 12,000원",
				},
			}},
			Usage: doc2vllmUsage{PromptTokens: 100, CompletionTokens: 30, TotalTokens: 130},
		},
	}}, nil, 20000)

	require.Contains(t, message, "### invoice.png")
	require.Contains(t, message, "- Prompt Tokens: `100`")
	require.Contains(t, message, "- Completion Tokens: `30`")
	require.Contains(t, message, "- Total Tokens: `130`")
	require.Contains(t, message, "총액 12,000원")
}

func TestBuildDocumentResponseMarkdownIncludesPartialFailures(t *testing.T) {
	message := buildDocumentResponseMarkdown("", []doc2vllmDocumentResult{{
		Attachment: botAttachment{Name: "report.pdf"},
		Response: doc2vllmOCRResponse{
			Model: "pdf-text-layer",
			Choices: []doc2vllmChoice{{
				Message: doc2vllmChoiceMessage{
					Role:    "assistant",
					Content: "추출된 본문",
				},
			}},
		},
		Source:    "pdf_text",
		Processor: "pdftotext",
	}}, []documentProcessingFailure{{
		AttachmentName: "appendix.pdf (page 2)",
		Message:        "OCR 요청이 실패했습니다.",
		Hint:           "잠시 후 다시 시도하세요.",
	}}, 20000)

	require.Contains(t, message, "- Source: `pdf_text`")
	require.Contains(t, message, "- Processor: `pdftotext`")
	require.Contains(t, message, "## Partial Failures (1)")
	require.Contains(t, message, "appendix.pdf (page 2)")
}

func TestBuildDocumentResponseOutputTextMode(t *testing.T) {
	message := buildDocumentResponseOutput("text", "", []doc2vllmDocumentResult{{
		Attachment: botAttachment{Name: "report.docx"},
		Response: doc2vllmOCRResponse{
			Model: "docx-text-layer",
			Choices: []doc2vllmChoice{{
				Message: doc2vllmChoiceMessage{
					Role:    "assistant",
					Content: "본문 내용",
				},
			}},
		},
		Source:    "docx_text",
		Processor: "zip+xml",
	}}, nil, 20000)

	require.Contains(t, message, "[report.docx]")
	require.Contains(t, message, "source=docx_text")
	require.Contains(t, message, "processor=zip+xml")
	require.Contains(t, message, "본문 내용")
}

func TestBuildDocumentResponseOutputJSONMode(t *testing.T) {
	message := buildDocumentResponseOutput("json", "요약해줘", []doc2vllmDocumentResult{{
		Attachment: botAttachment{Name: "invoice.png"},
		Response: doc2vllmOCRResponse{
			Model: "doc2vllm-ocr",
			Choices: []doc2vllmChoice{{
				Message: doc2vllmChoiceMessage{
					Role:    "assistant",
					Content: "총액 12000원",
				},
			}},
		},
		Source: "ocr",
	}}, []documentProcessingFailure{{
		AttachmentName: "report.pdf (page 2)",
		Message:        "페이지 처리 실패",
	}}, 20000)

	require.Contains(t, message, `"prompt": "요약해줘"`)
	require.Contains(t, message, `"name": "invoice.png"`)
	require.Contains(t, message, `"source": "ocr"`)
	require.Contains(t, message, `"failures"`)
}

func TestBuildBotResponseMessageIncludesAPIDuration(t *testing.T) {
	message := buildBotResponseMessage("파싱 완료", "corr-123", 2350*time.Millisecond)

	require.Contains(t, message, "_Correlation ID:_ `corr-123`")
	require.Contains(t, message, "_Doc2VLLM API 응답 시간:_ `2.35초`")
}

func TestBuildBotFailureMessageIncludesAPIDuration(t *testing.T) {
	message := buildBotFailureMessage(BotDefinition{Model: "doc2vllm-ocr"}, "corr-123", executionFailureView{
		Message:     "실패",
		APIDuration: 12750 * time.Millisecond,
	})

	require.Contains(t, message, "_Correlation ID:_ `corr-123`")
	require.Contains(t, message, "_Doc2VLLM API 응답 시간:_ `12.8초`")
}

func TestExtractTextFromBody(t *testing.T) {
	body := []byte(`{"error":{"message":"invalid api key"}}`)
	require.Equal(t, "invalid api key", extractTextFromBody(body))
}

func TestServiceConfigForBotPrefersBotOverrides(t *testing.T) {
	parsedURL, err := url.Parse("http://localhost:8000/v1/chat/completions")
	require.NoError(t, err)

	cfg := &runtimeConfiguration{
		ServiceBaseURL: "http://localhost:8000/v1/chat/completions",
		ParsedBaseURL:  parsedURL,
		AuthMode:       "bearer",
		AuthToken:      "secret",
		AllowHosts:     []string{"localhost"},
		DefaultTimeout: 45 * time.Second,
	}

	service, err := cfg.serviceConfigForBot(BotDefinition{
		BaseURL:   "http://localhost:8000/v1",
		AuthMode:  "x-api-key",
		AuthToken: "override",
	})
	require.NoError(t, err)
	require.Equal(t, "http://localhost:8000/v1/chat/completions", service.BaseURL)
	require.Equal(t, "x-api-key", service.AuthMode)
	require.Equal(t, "override", service.AuthToken)
	require.Equal(t, 45*time.Second, service.Timeout)
}

func TestClassifyDoc2VLLMHTTPErrorUnauthorized(t *testing.T) {
	err := classifyDoc2VLLMHTTPError(
		"http://localhost:8000/v1/chat/completions",
		401,
		nil,
		[]byte(`{"detail":"invalid token"}`),
	)

	require.Equal(t, "auth_failed", err.Code)
	require.Equal(t, "Doc2VLLM 인증에 실패했습니다.", err.Summary)
	require.Contains(t, err.Detail, "invalid token")
	require.False(t, err.Retryable)
}

func TestClassifyDoc2VLLMRequestErrorTimeout(t *testing.T) {
	err := classifyDoc2VLLMRequestError(
		"http://localhost:8000/v1/chat/completions",
		context.DeadlineExceeded,
	)

	require.Equal(t, "network_timeout", err.Code)
	require.True(t, err.Retryable)
	require.Contains(t, err.Error(), "시간 초과")
}

func TestRenderVLLMPromptSupportsPlaceholders(t *testing.T) {
	rendered := renderVLLMPrompt(
		"사용자 요청:\n{{user_message}}\n\n문서:\n{{document_text}}",
		"표를 요약해줘",
		"문서 내용",
	)

	require.Contains(t, rendered, "표를 요약해줘")
	require.Contains(t, rendered, "문서 내용")
}

func TestBuildVLLMFallbackOutputIncludesDocumentContext(t *testing.T) {
	output := buildVLLMFallbackOutput("### invoice.png\n본문", "vLLM 후처리에 실패해 Doc2VLLM OCR 결과를 대신 표시합니다.")

	require.Contains(t, output, "Doc2VLLM OCR 결과를 대신 표시합니다.")
	require.Contains(t, output, "### invoice.png")
	require.Contains(t, output, "본문")
}

func TestExtractPromptFromMessageTriggersFileOnlyDirectMessages(t *testing.T) {
	bot, err := (BotDefinition{Username: "parser-bot", DisplayName: "Parser Bot"}).normalize()
	require.NoError(t, err)

	plugin := &Plugin{}
	plugin.setBotAccounts(map[string]botAccount{
		bot.ID: {
			Definition: bot,
			UserID:     "bot-user-id",
		},
	})

	cfg := &runtimeConfiguration{BotDefinitions: []BotDefinition{bot}}
	channel := &model.Channel{
		Type: model.ChannelTypeDirect,
		Name: "bot-user-id__human-user-id",
	}

	triggeredBot, prompt, triggered := plugin.extractPromptFromMessage(cfg, channel, "")
	require.True(t, triggered)
	require.NotNil(t, triggeredBot)
	require.Equal(t, bot.ID, triggeredBot.ID)
	require.Empty(t, prompt)
}

func TestExtractPromptFromMessageIgnoresEmptyNonDirectMessages(t *testing.T) {
	bot, err := (BotDefinition{Username: "parser-bot", DisplayName: "Parser Bot"}).normalize()
	require.NoError(t, err)

	plugin := &Plugin{}
	cfg := &runtimeConfiguration{BotDefinitions: []BotDefinition{bot}}
	channel := &model.Channel{
		Type: model.ChannelTypeOpen,
		Name: "town-square",
	}

	triggeredBot, prompt, triggered := plugin.extractPromptFromMessage(cfg, channel, "")
	require.False(t, triggered)
	require.Nil(t, triggeredBot)
	require.Empty(t, prompt)
}

func buildTestDOCX(t *testing.T, files map[string]string) []byte {
	return buildTestOOXMLArchive(t, files)
}

func buildTestOOXMLArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		entry, err := writer.Create(name)
		require.NoError(t, err)
		_, err = entry.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return buffer.Bytes()
}
