package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

func isPPTXAttachment(attachment botAttachment) bool {
	mimeType := strings.ToLower(strings.TrimSpace(attachment.MIMEType))
	if mimeType == "application/vnd.openxmlformats-officedocument.presentationml.presentation" {
		return true
	}
	return strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(attachment.Extension), "."), "pptx")
}

func extractTextFromPPTXAttachment(attachment botAttachment) (doc2vllmDocumentResult, error) {
	if len(attachment.Content) == 0 {
		return doc2vllmDocumentResult{}, newDoc2VLLMCallError(
			"empty_attachment",
			"빈 PPTX 파일은 처리할 수 없습니다.",
			sanitizeUploadFilename(attachment.Name),
			"PPTX 파일이 정상적으로 업로드되었는지 확인하세요.",
			"",
			0,
			false,
		)
	}

	reader, err := zip.NewReader(bytes.NewReader(attachment.Content), int64(len(attachment.Content)))
	if err != nil {
		return doc2vllmDocumentResult{}, newDoc2VLLMCallError(
			"pptx_decode_failed",
			"PPTX 파일을 열지 못했습니다.",
			err.Error(),
			"손상되지 않은 PPTX 파일인지 확인하세요.",
			"",
			0,
			false,
		)
	}

	slideFiles := collectZIPFilesByPrefix(reader.File, "ppt/slides/", ".xml")
	if len(slideFiles) == 0 {
		return doc2vllmDocumentResult{}, newDoc2VLLMCallError(
			"pptx_text_missing",
			"PPTX 본문에서 읽을 수 있는 슬라이드를 찾지 못했습니다.",
			sanitizeUploadFilename(attachment.Name),
			"텍스트가 포함된 PPTX 파일인지 확인하세요.",
			"",
			0,
			false,
		)
	}

	sections := make([]string, 0, len(slideFiles))
	for index, slideFile := range slideFiles {
		content, readErr := readZipFileText(slideFile)
		if readErr != nil {
			return doc2vllmDocumentResult{}, newDoc2VLLMCallError(
				"pptx_decode_failed",
				"PPTX 슬라이드 XML을 읽지 못했습니다.",
				readErr.Error(),
				"PPTX 파일이 손상되지 않았는지 확인하세요.",
				"",
				0,
				false,
			)
		}

		text, extractErr := extractPresentationMLText(content)
		if extractErr != nil {
			return doc2vllmDocumentResult{}, newDoc2VLLMCallError(
				"pptx_decode_failed",
				"PPTX 슬라이드 텍스트를 해석하지 못했습니다.",
				extractErr.Error(),
				"PPTX 파일 형식과 내용을 확인하세요.",
				"",
				0,
				false,
			)
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		sections = append(sections, fmt.Sprintf("## Slide %d\n%s", index+1, text))
	}

	combined := strings.TrimSpace(strings.Join(sections, "\n\n"))
	if combined == "" {
		return doc2vllmDocumentResult{}, newDoc2VLLMCallError(
			"pptx_text_missing",
			"PPTX 본문에서 읽을 수 있는 텍스트를 찾지 못했습니다.",
			sanitizeUploadFilename(attachment.Name),
			"텍스트가 포함된 PPTX 파일인지 확인하세요.",
			"",
			0,
			false,
		)
	}

	return newDirectTextDocumentResult(attachment, combined, "pptx-text-layer", "pptx_text", "zip+xml"), nil
}

func extractPresentationMLText(raw []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var builder strings.Builder

	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}

		switch typed := token.(type) {
		case xml.StartElement:
			switch typed.Name.Local {
			case "t":
				var text string
				if err := decoder.DecodeElement(&text, &typed); err != nil {
					return "", err
				}
				builder.WriteString(text)
			case "br":
				builder.WriteString("\n")
			}
		case xml.EndElement:
			switch typed.Name.Local {
			case "p", "sp":
				builder.WriteString("\n")
			}
		}
	}

	return normalizeOfficeText(builder.String()), nil
}
