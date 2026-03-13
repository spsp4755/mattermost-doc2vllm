package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var (
	extractPDFAttachmentText     = extractTextFromPDFAttachment
	convertPDFAttachmentToImages = rasterizePDFAttachment
)

type pdfProcessingOptions struct {
	RasterDPI int
	MaxPages  int
}

type preparedOCRInput struct {
	Attachment   botAttachment
	DirectResult *doc2vllmDocumentResult
}

type documentProcessingFailure struct {
	AttachmentName string
	ErrorCode      string
	Message        string
	Detail         string
	Hint           string
	HTTPStatus     int
	Retryable      bool
}

type pdfRasterizer struct {
	Name    string
	Command string
	Args    func(inputPath, outputDir string, options pdfProcessingOptions) []string
}

type pdfTextExtractor struct {
	Name    string
	Command string
	Args    func(inputPath string, options pdfProcessingOptions) []string
}

type pdfTextExtraction struct {
	Text      string
	Processor string
}

func (p *Plugin) prepareOCRInputs(ctx context.Context, cfg *runtimeConfiguration, attachments []botAttachment) ([]preparedOCRInput, []documentProcessingFailure) {
	options := resolvePDFProcessingOptions(cfg)
	prepared := make([]preparedOCRInput, 0, len(attachments))
	failures := make([]documentProcessingFailure, 0)

	for _, attachment := range attachments {
		switch {
		case isImageAttachment(attachment):
			prepared = append(prepared, preparedOCRInput{Attachment: attachment})
		case isPDFAttachment(attachment):
			pdfInputs, err := preparePDFAttachment(ctx, attachment, options)
			if err != nil {
				failures = append(failures, newDocumentProcessingFailure(attachment.Name, err))
				continue
			}
			prepared = append(prepared, pdfInputs...)
		case isDOCXAttachment(attachment):
			result, err := extractTextFromDOCXAttachment(attachment)
			if err != nil {
				failures = append(failures, newDocumentProcessingFailure(attachment.Name, err))
				continue
			}
			prepared = append(prepared, preparedOCRInput{DirectResult: &result})
		case isXLSXAttachment(attachment):
			result, err := extractTextFromXLSXAttachment(attachment)
			if err != nil {
				failures = append(failures, newDocumentProcessingFailure(attachment.Name, err))
				continue
			}
			prepared = append(prepared, preparedOCRInput{DirectResult: &result})
		case isPPTXAttachment(attachment):
			result, err := extractTextFromPPTXAttachment(attachment)
			if err != nil {
				failures = append(failures, newDocumentProcessingFailure(attachment.Name, err))
				continue
			}
			prepared = append(prepared, preparedOCRInput{DirectResult: &result})
		default:
			failures = append(failures, newDocumentProcessingFailure(attachment.Name, newDoc2VLLMCallError(
				"unsupported_media_type",
				"Doc2VLLM OCR은 이미지, PDF, DOCX, XLSX, PPTX 첨부를 지원합니다.",
				fmt.Sprintf("현재 파일 형식: %s", describeAttachmentType(attachment)),
				"PNG, JPG, WEBP, PDF, DOCX, XLSX 또는 PPTX 파일을 첨부해 주세요.",
				"",
				http.StatusUnsupportedMediaType,
				false,
			)))
		}
	}

	return prepared, failures
}

func resolvePDFProcessingOptions(cfg *runtimeConfiguration) pdfProcessingOptions {
	options := pdfProcessingOptions{
		RasterDPI: defaultPDFRasterDPI,
		MaxPages:  defaultMaxPDFPages,
	}
	if cfg == nil {
		return options
	}
	options.RasterDPI = positiveOrDefault(cfg.PDFRasterDPI, defaultPDFRasterDPI)
	options.MaxPages = positiveOrDefault(cfg.MaxPDFPages, defaultMaxPDFPages)
	return options
}

func preparePDFAttachment(ctx context.Context, attachment botAttachment, options pdfProcessingOptions) ([]preparedOCRInput, error) {
	options = normalizePDFProcessingOptions(options)
	if len(attachment.Content) == 0 {
		return nil, newDoc2VLLMCallError(
			"empty_attachment",
			"빈 PDF 파일은 처리할 수 없습니다.",
			sanitizeUploadFilename(attachment.Name),
			"PDF 파일이 정상적으로 업로드되었는지 확인하세요.",
			"",
			0,
			false,
		)
	}

	extracted, err := extractPDFAttachmentText(ctx, attachment, options)
	if err == nil && strings.TrimSpace(extracted.Text) != "" {
		result := newPDFTextDocumentResult(attachment, extracted.Text, extracted.Processor)
		return []preparedOCRInput{{DirectResult: &result}}, nil
	}

	pages, err := convertPDFAttachmentToImages(ctx, attachment, options)
	if err != nil {
		return nil, err
	}

	prepared := make([]preparedOCRInput, 0, len(pages))
	for _, page := range pages {
		prepared = append(prepared, preparedOCRInput{Attachment: page})
	}
	return prepared, nil
}

func normalizePDFProcessingOptions(options pdfProcessingOptions) pdfProcessingOptions {
	options.RasterDPI = positiveOrDefault(options.RasterDPI, defaultPDFRasterDPI)
	options.MaxPages = positiveOrDefault(options.MaxPages, defaultMaxPDFPages)
	return options
}

func isImageAttachment(attachment botAttachment) bool {
	mimeType := strings.ToLower(strings.TrimSpace(attachment.MIMEType))
	return strings.HasPrefix(mimeType, "image/")
}

func isPDFAttachment(attachment botAttachment) bool {
	mimeType := strings.ToLower(strings.TrimSpace(attachment.MIMEType))
	if mimeType == "application/pdf" || mimeType == "application/x-pdf" {
		return true
	}
	return strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(attachment.Extension), "."), "pdf")
}

func describeAttachmentType(attachment botAttachment) string {
	if mimeType := strings.TrimSpace(attachment.MIMEType); mimeType != "" {
		return mimeType
	}
	if ext := strings.TrimSpace(attachment.Extension); ext != "" {
		return "." + strings.TrimPrefix(ext, ".")
	}
	return "unknown"
}

func extractTextFromPDFAttachment(ctx context.Context, attachment botAttachment, options pdfProcessingOptions) (pdfTextExtraction, error) {
	options = normalizePDFProcessingOptions(options)
	extractor, commandPath, err := detectPDFTextExtractor()
	if err != nil {
		return pdfTextExtraction{}, err
	}

	tempDir, err := os.MkdirTemp("", "doc2vllm-pdf-text-*")
	if err != nil {
		return pdfTextExtraction{}, fmt.Errorf("failed to create PDF text temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	inputPath, err := writePDFTempFile(tempDir, attachment)
	if err != nil {
		return pdfTextExtraction{}, err
	}

	command := exec.CommandContext(ctx, commandPath, extractor.Args(inputPath, options)...)
	output, err := command.CombinedOutput()
	if err != nil {
		return pdfTextExtraction{}, err
	}

	text := normalizeExtractedPDFText(string(output))
	if text == "" {
		return pdfTextExtraction{}, nil
	}
	return pdfTextExtraction{
		Text:      text,
		Processor: extractor.Name,
	}, nil
}

func rasterizePDFAttachment(ctx context.Context, attachment botAttachment, options pdfProcessingOptions) ([]botAttachment, error) {
	options = normalizePDFProcessingOptions(options)
	if len(attachment.Content) == 0 {
		return nil, newDoc2VLLMCallError(
			"empty_attachment",
			"빈 PDF 파일은 처리할 수 없습니다.",
			sanitizeUploadFilename(attachment.Name),
			"PDF 파일이 정상적으로 업로드되었는지 확인하세요.",
			"",
			0,
			false,
		)
	}

	rasterizer, commandPath, err := detectPDFRasterizer()
	if err != nil {
		return nil, newDoc2VLLMCallError(
			"pdf_converter_missing",
			"PDF를 이미지로 변환할 수 있는 도구를 찾지 못했습니다.",
			err.Error(),
			"서버에 pdftoppm, mutool, magick 또는 ghostscript(gs/gswin64c)를 설치해 주세요.",
			"",
			0,
			false,
		)
	}

	tempDir, err := os.MkdirTemp("", "doc2vllm-pdf-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create PDF temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	inputPath, err := writePDFTempFile(tempDir, attachment)
	if err != nil {
		return nil, err
	}

	command := exec.CommandContext(ctx, commandPath, rasterizer.Args(inputPath, tempDir, options)...)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, newDoc2VLLMCallError(
			"pdf_conversion_failed",
			"PDF를 이미지로 변환하지 못했습니다.",
			strings.TrimSpace(string(output)),
			fmt.Sprintf("%s 실행 경로와 PDF 렌더링 지원 여부를 확인하세요.", rasterizer.Name),
			"",
			0,
			true,
		)
	}

	imagePaths, err := collectRasterizedImages(tempDir)
	if err != nil {
		return nil, fmt.Errorf("failed to collect rasterized PDF pages: %w", err)
	}
	if len(imagePaths) == 0 {
		return nil, newDoc2VLLMCallError(
			"pdf_conversion_failed",
			"PDF를 이미지로 변환했지만 페이지 이미지를 찾지 못했습니다.",
			rasterizer.Name,
			"PDF 변환 도구가 PNG 파일을 생성하는지 확인하세요.",
			"",
			0,
			true,
		)
	}
	if options.MaxPages > 0 && len(imagePaths) > options.MaxPages {
		imagePaths = imagePaths[:options.MaxPages]
	}

	pages := make([]botAttachment, 0, len(imagePaths))
	for index, imagePath := range imagePaths {
		content, readErr := os.ReadFile(imagePath)
		if readErr != nil {
			return nil, fmt.Errorf("failed to read rasterized PDF page: %w", readErr)
		}
		pages = append(pages, botAttachment{
			Name:      fmt.Sprintf("%s (page %d)", defaultIfEmpty(strings.TrimSpace(attachment.Name), "document.pdf"), index+1),
			MIMEType:  "image/png",
			Extension: "png",
			Size:      int64(len(content)),
			Content:   content,
		})
	}

	return pages, nil
}

func writePDFTempFile(tempDir string, attachment botAttachment) (string, error) {
	inputPath := filepath.Join(tempDir, sanitizeUploadFilename(defaultIfEmpty(attachment.Name, "document.pdf")))
	if !strings.HasSuffix(strings.ToLower(inputPath), ".pdf") {
		inputPath += ".pdf"
	}
	if err := os.WriteFile(inputPath, attachment.Content, 0600); err != nil {
		return "", fmt.Errorf("failed to write PDF temp file: %w", err)
	}
	return inputPath, nil
}

func detectPDFTextExtractor() (pdfTextExtractor, string, error) {
	for _, extractor := range supportedPDFTextExtractors() {
		commandPath, err := exec.LookPath(extractor.Command)
		if err == nil {
			return extractor, commandPath, nil
		}
	}
	return pdfTextExtractor{}, "", fmt.Errorf("no supported PDF text extractor found")
}

func supportedPDFTextExtractors() []pdfTextExtractor {
	return []pdfTextExtractor{
		{
			Name:    "pdftotext",
			Command: "pdftotext",
			Args: func(inputPath string, options pdfProcessingOptions) []string {
				args := []string{"-enc", "UTF-8", "-nopgbrk", "-f", "1"}
				if options.MaxPages > 0 {
					args = append(args, "-l", strconv.Itoa(options.MaxPages))
				}
				args = append(args, inputPath, "-")
				return args
			},
		},
	}
}

func detectPDFRasterizer() (pdfRasterizer, string, error) {
	for _, rasterizer := range supportedPDFRasterizers() {
		commandPath, err := exec.LookPath(rasterizer.Command)
		if err == nil {
			return rasterizer, commandPath, nil
		}
	}
	return pdfRasterizer{}, "", fmt.Errorf("no supported PDF rasterizer found")
}

func supportedPDFRasterizers() []pdfRasterizer {
	return []pdfRasterizer{
		{
			Name:    "pdftoppm",
			Command: "pdftoppm",
			Args: func(inputPath, outputDir string, options pdfProcessingOptions) []string {
				args := []string{"-png", "-r", strconv.Itoa(options.RasterDPI), "-f", "1"}
				if options.MaxPages > 0 {
					args = append(args, "-l", strconv.Itoa(options.MaxPages))
				}
				args = append(args, inputPath, filepath.Join(outputDir, "page"))
				return args
			},
		},
		{
			Name:    "mutool",
			Command: "mutool",
			Args: func(inputPath, outputDir string, options pdfProcessingOptions) []string {
				args := []string{"draw", "-q", "-r", strconv.Itoa(options.RasterDPI), "-o", filepath.Join(outputDir, "page-%d.png"), inputPath}
				if options.MaxPages > 0 {
					args = append(args, fmt.Sprintf("1-%d", options.MaxPages))
				}
				return args
			},
		},
		{
			Name:    "magick",
			Command: "magick",
			Args: func(inputPath, outputDir string, options pdfProcessingOptions) []string {
				source := inputPath
				if options.MaxPages > 0 {
					source = fmt.Sprintf("%s[0-%d]", inputPath, options.MaxPages-1)
				}
				return []string{"-density", strconv.Itoa(options.RasterDPI), source, filepath.Join(outputDir, "page-%d.png")}
			},
		},
		{
			Name:    "gswin64c",
			Command: "gswin64c",
			Args: func(inputPath, outputDir string, options pdfProcessingOptions) []string {
				args := []string{"-dSAFER", "-dBATCH", "-dNOPAUSE", "-sDEVICE=png16m", "-r" + strconv.Itoa(options.RasterDPI), "-dFirstPage=1"}
				if options.MaxPages > 0 {
					args = append(args, "-dLastPage="+strconv.Itoa(options.MaxPages))
				}
				args = append(args, "-o", filepath.Join(outputDir, "page-%d.png"), inputPath)
				return args
			},
		},
		{
			Name:    "gswin32c",
			Command: "gswin32c",
			Args: func(inputPath, outputDir string, options pdfProcessingOptions) []string {
				args := []string{"-dSAFER", "-dBATCH", "-dNOPAUSE", "-sDEVICE=png16m", "-r" + strconv.Itoa(options.RasterDPI), "-dFirstPage=1"}
				if options.MaxPages > 0 {
					args = append(args, "-dLastPage="+strconv.Itoa(options.MaxPages))
				}
				args = append(args, "-o", filepath.Join(outputDir, "page-%d.png"), inputPath)
				return args
			},
		},
		{
			Name:    "gs",
			Command: "gs",
			Args: func(inputPath, outputDir string, options pdfProcessingOptions) []string {
				args := []string{"-dSAFER", "-dBATCH", "-dNOPAUSE", "-sDEVICE=png16m", "-r" + strconv.Itoa(options.RasterDPI), "-dFirstPage=1"}
				if options.MaxPages > 0 {
					args = append(args, "-dLastPage="+strconv.Itoa(options.MaxPages))
				}
				args = append(args, "-o", filepath.Join(outputDir, "page-%d.png"), inputPath)
				return args
			},
		},
	}
}

func collectRasterizedImages(outputDir string) ([]string, error) {
	imagePaths, err := filepath.Glob(filepath.Join(outputDir, "*.png"))
	if err != nil {
		return nil, err
	}

	sort.Slice(imagePaths, func(i, j int) bool {
		leftPage, leftOK := rasterizedPageNumber(imagePaths[i])
		rightPage, rightOK := rasterizedPageNumber(imagePaths[j])
		switch {
		case leftOK && rightOK && leftPage != rightPage:
			return leftPage < rightPage
		default:
			return imagePaths[i] < imagePaths[j]
		}
	})
	return imagePaths, nil
}

func normalizeExtractedPDFText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\x00", "")
	return strings.TrimSpace(value)
}

func newDocumentProcessingFailure(attachmentName string, err error) documentProcessingFailure {
	failure := documentProcessingFailure{
		AttachmentName: sanitizeUploadFilename(defaultIfEmpty(strings.TrimSpace(attachmentName), "attachment")),
		Message:        strings.TrimSpace(err.Error()),
	}

	var callErr *doc2vllmCallError
	if errors.As(err, &callErr) {
		failure.ErrorCode = strings.TrimSpace(callErr.Code)
		failure.Message = strings.TrimSpace(callErr.Summary)
		failure.Detail = strings.TrimSpace(callErr.Detail)
		failure.Hint = strings.TrimSpace(callErr.Hint)
		failure.HTTPStatus = callErr.StatusCode
		failure.Retryable = callErr.Retryable
	}

	return failure
}

func rasterizedPageNumber(path string) (int, bool) {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	index := strings.LastIndex(base, "-")
	if index < 0 || index >= len(base)-1 {
		return 0, false
	}
	page, err := strconv.Atoi(base[index+1:])
	if err != nil {
		return 0, false
	}
	return page, true
}
