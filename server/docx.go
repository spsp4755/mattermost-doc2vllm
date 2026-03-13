package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"sort"
	"strings"
)

func isDOCXAttachment(attachment botAttachment) bool {
	mimeType := strings.ToLower(strings.TrimSpace(attachment.MIMEType))
	if mimeType == "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		return true
	}
	return strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(attachment.Extension), "."), "docx")
}

func extractTextFromDOCXAttachment(attachment botAttachment) (doc2vllmDocumentResult, error) {
	if len(attachment.Content) == 0 {
		return doc2vllmDocumentResult{}, newDoc2VLLMCallError(
			"empty_attachment",
			"빈 DOCX 파일은 처리할 수 없습니다.",
			sanitizeUploadFilename(attachment.Name),
			"DOCX 파일이 정상적으로 업로드되었는지 확인하세요.",
			"",
			0,
			false,
		)
	}

	reader, err := zip.NewReader(bytes.NewReader(attachment.Content), int64(len(attachment.Content)))
	if err != nil {
		return doc2vllmDocumentResult{}, newDoc2VLLMCallError(
			"docx_decode_failed",
			"DOCX 파일을 열지 못했습니다.",
			err.Error(),
			"손상되지 않은 DOCX 파일인지 확인하세요.",
			"",
			0,
			false,
		)
	}

	xmlFiles := collectDOCXXMLFiles(reader.File)
	if len(xmlFiles) == 0 {
		return doc2vllmDocumentResult{}, newDoc2VLLMCallError(
			"docx_text_missing",
			"DOCX 본문에서 읽을 수 있는 텍스트를 찾지 못했습니다.",
			sanitizeUploadFilename(attachment.Name),
			"텍스트가 포함된 DOCX 파일인지 확인하세요.",
			"",
			0,
			false,
		)
	}

	parts := make([]string, 0, len(xmlFiles))
	for _, file := range xmlFiles {
		content, readErr := readZipFileText(file)
		if readErr != nil {
			return doc2vllmDocumentResult{}, newDoc2VLLMCallError(
				"docx_decode_failed",
				"DOCX 내부 XML을 읽지 못했습니다.",
				readErr.Error(),
				"DOCX 파일이 손상되지 않았는지 확인하세요.",
				"",
				0,
				false,
			)
		}
		text, extractErr := extractWordprocessingMLText(content)
		if extractErr != nil {
			return doc2vllmDocumentResult{}, newDoc2VLLMCallError(
				"docx_decode_failed",
				"DOCX 텍스트를 해석하지 못했습니다.",
				extractErr.Error(),
				"DOCX 파일 형식과 내용을 확인하세요.",
				"",
				0,
				false,
			)
		}
		if strings.TrimSpace(text) != "" {
			parts = append(parts, text)
		}
	}

	combined := strings.TrimSpace(strings.Join(parts, "\n\n"))
	if combined == "" {
		return doc2vllmDocumentResult{}, newDoc2VLLMCallError(
			"docx_text_missing",
			"DOCX 본문에서 읽을 수 있는 텍스트를 찾지 못했습니다.",
			sanitizeUploadFilename(attachment.Name),
			"텍스트가 포함된 DOCX 파일인지 확인하세요.",
			"",
			0,
			false,
		)
	}

	return newDirectTextDocumentResult(attachment, combined, "docx-text-layer", "docx_text", "zip+xml"), nil
}

func collectDOCXXMLFiles(files []*zip.File) []*zip.File {
	collected := make([]*zip.File, 0)
	for _, file := range files {
		switch {
		case file == nil:
			continue
		case file.Name == "word/document.xml":
			collected = append(collected, file)
		case strings.HasPrefix(file.Name, "word/header") && strings.HasSuffix(file.Name, ".xml"):
			collected = append(collected, file)
		case strings.HasPrefix(file.Name, "word/footer") && strings.HasSuffix(file.Name, ".xml"):
			collected = append(collected, file)
		}
	}

	sort.Slice(collected, func(i, j int) bool {
		return collected[i].Name < collected[j].Name
	})
	return collected
}

func readZipFileText(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func extractWordprocessingMLText(raw []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var builder strings.Builder

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		switch typed := token.(type) {
		case xml.StartElement:
			switch typed.Name.Local {
			case "t":
				var text string
				if decodeErr := decoder.DecodeElement(&text, &typed); decodeErr != nil {
					return "", decodeErr
				}
				builder.WriteString(text)
			case "tab":
				builder.WriteString("\t")
			case "br", "cr":
				builder.WriteString("\n")
			}
		case xml.EndElement:
			switch typed.Name.Local {
			case "p", "tbl":
				builder.WriteString("\n")
			case "tr":
				builder.WriteString("\n")
			case "tc":
				builder.WriteString("\t")
			}
		}
	}

	return normalizeDOCXText(builder.String()), nil
}

func normalizeDOCXText(value string) string {
	return normalizeOfficeText(value)
}
