package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
)

func isXLSXAttachment(attachment botAttachment) bool {
	mimeType := strings.ToLower(strings.TrimSpace(attachment.MIMEType))
	if mimeType == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		return true
	}
	return strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(attachment.Extension), "."), "xlsx")
}

func extractTextFromXLSXAttachment(attachment botAttachment) (doc2vllmDocumentResult, error) {
	if len(attachment.Content) == 0 {
		return doc2vllmDocumentResult{}, newDoc2VLLMCallError(
			"empty_attachment",
			"빈 XLSX 파일은 처리할 수 없습니다.",
			sanitizeUploadFilename(attachment.Name),
			"XLSX 파일이 정상적으로 업로드되었는지 확인하세요.",
			"",
			0,
			false,
		)
	}

	reader, err := zip.NewReader(bytes.NewReader(attachment.Content), int64(len(attachment.Content)))
	if err != nil {
		return doc2vllmDocumentResult{}, newDoc2VLLMCallError(
			"xlsx_decode_failed",
			"XLSX 파일을 열지 못했습니다.",
			err.Error(),
			"손상되지 않은 XLSX 파일인지 확인하세요.",
			"",
			0,
			false,
		)
	}

	filesByName := make(map[string]*zip.File, len(reader.File))
	for _, file := range reader.File {
		if file != nil {
			filesByName[file.Name] = file
		}
	}

	sharedStrings, err := readXLSXSharedStrings(filesByName["xl/sharedStrings.xml"])
	if err != nil {
		return doc2vllmDocumentResult{}, newDoc2VLLMCallError(
			"xlsx_decode_failed",
			"XLSX shared strings를 읽지 못했습니다.",
			err.Error(),
			"XLSX 파일 형식을 확인하세요.",
			"",
			0,
			false,
		)
	}

	sheetNames, err := readXLSXSheetNames(filesByName)
	if err != nil {
		return doc2vllmDocumentResult{}, newDoc2VLLMCallError(
			"xlsx_decode_failed",
			"XLSX 시트 정보를 읽지 못했습니다.",
			err.Error(),
			"XLSX 파일 형식을 확인하세요.",
			"",
			0,
			false,
		)
	}

	sheetFiles := collectZIPFilesByPrefix(reader.File, "xl/worksheets/", ".xml")
	if len(sheetFiles) == 0 {
		return doc2vllmDocumentResult{}, newDoc2VLLMCallError(
			"xlsx_text_missing",
			"XLSX 본문에서 읽을 수 있는 시트를 찾지 못했습니다.",
			sanitizeUploadFilename(attachment.Name),
			"텍스트가 포함된 XLSX 파일인지 확인하세요.",
			"",
			0,
			false,
		)
	}

	sections := make([]string, 0, len(sheetFiles))
	for _, sheetFile := range sheetFiles {
		content, readErr := readZipFileText(sheetFile)
		if readErr != nil {
			return doc2vllmDocumentResult{}, newDoc2VLLMCallError(
				"xlsx_decode_failed",
				"XLSX 시트 XML을 읽지 못했습니다.",
				readErr.Error(),
				"XLSX 파일이 손상되지 않았는지 확인하세요.",
				"",
				0,
				false,
			)
		}

		sheetText, extractErr := extractSpreadsheetMLText(content, sharedStrings)
		if extractErr != nil {
			return doc2vllmDocumentResult{}, newDoc2VLLMCallError(
				"xlsx_decode_failed",
				"XLSX 시트 텍스트를 해석하지 못했습니다.",
				extractErr.Error(),
				"XLSX 파일 형식과 내용을 확인하세요.",
				"",
				0,
				false,
			)
		}
		if strings.TrimSpace(sheetText) == "" {
			continue
		}

		sheetName := sheetNames[sheetFile.Name]
		if strings.TrimSpace(sheetName) == "" {
			sheetName = path.Base(sheetFile.Name)
		}
		sections = append(sections, strings.TrimSpace("## Sheet: "+sheetName+"\n"+sheetText))
	}

	combined := strings.TrimSpace(strings.Join(sections, "\n\n"))
	if combined == "" {
		return doc2vllmDocumentResult{}, newDoc2VLLMCallError(
			"xlsx_text_missing",
			"XLSX 본문에서 읽을 수 있는 텍스트를 찾지 못했습니다.",
			sanitizeUploadFilename(attachment.Name),
			"텍스트가 포함된 XLSX 파일인지 확인하세요.",
			"",
			0,
			false,
		)
	}

	return newDirectTextDocumentResult(attachment, combined, "xlsx-text-layer", "xlsx_text", "zip+xml"), nil
}

func readXLSXSharedStrings(file *zip.File) ([]string, error) {
	if file == nil {
		return nil, nil
	}

	content, err := readZipFileText(file)
	if err != nil {
		return nil, err
	}

	decoder := xml.NewDecoder(bytes.NewReader(content))
	stringsList := []string{}

	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "si" {
			continue
		}

		var builder strings.Builder
		if err := collectElementText(decoder, start.Name.Local, &builder, true); err != nil {
			return nil, err
		}
		stringsList = append(stringsList, normalizeOfficeText(builder.String()))
	}

	return stringsList, nil
}

func readXLSXSheetNames(filesByName map[string]*zip.File) (map[string]string, error) {
	workbookFile := filesByName["xl/workbook.xml"]
	if workbookFile == nil {
		return map[string]string{}, nil
	}

	relationshipTargets, err := readOOXMLRelationships(filesByName["xl/_rels/workbook.xml.rels"], "xl")
	if err != nil {
		return nil, err
	}

	content, err := readZipFileText(workbookFile)
	if err != nil {
		return nil, err
	}

	type workbookSheet struct {
		Name string `xml:"name,attr"`
		ID   string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
	}
	type workbook struct {
		Sheets []workbookSheet `xml:"sheets>sheet"`
	}

	var parsed workbook
	if err := xml.Unmarshal(content, &parsed); err != nil {
		return nil, err
	}

	result := make(map[string]string, len(parsed.Sheets))
	for _, sheet := range parsed.Sheets {
		target := relationshipTargets[sheet.ID]
		if target == "" {
			continue
		}
		result[target] = strings.TrimSpace(sheet.Name)
	}
	return result, nil
}

func extractSpreadsheetMLText(raw []byte, sharedStrings []string) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	rows := make([]string, 0)

	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}

		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "row" {
			continue
		}

		rowCells, err := parseSpreadsheetRow(decoder, sharedStrings)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(rowCells) != "" {
			rows = append(rows, rowCells)
		}
	}

	return strings.TrimSpace(strings.Join(rows, "\n")), nil
}

func parseSpreadsheetRow(decoder *xml.Decoder, sharedStrings []string) (string, error) {
	values := map[int]string{}
	maxCol := -1

	for {
		token, err := decoder.Token()
		if err != nil {
			return "", err
		}

		switch typed := token.(type) {
		case xml.StartElement:
			if typed.Name.Local != "c" {
				continue
			}
			columnIndex, cellValue, err := parseSpreadsheetCell(decoder, typed, sharedStrings)
			if err != nil {
				return "", err
			}
			if columnIndex < 0 {
				columnIndex = maxCol + 1
			}
			values[columnIndex] = strings.TrimSpace(cellValue)
			if columnIndex > maxCol {
				maxCol = columnIndex
			}
		case xml.EndElement:
			if typed.Name.Local != "row" {
				continue
			}
			if maxCol < 0 {
				return "", nil
			}
			cells := make([]string, maxCol+1)
			lastNonEmpty := -1
			for index := 0; index <= maxCol; index++ {
				cells[index] = values[index]
				if strings.TrimSpace(cells[index]) != "" {
					lastNonEmpty = index
				}
			}
			if lastNonEmpty < 0 {
				return "", nil
			}
			return strings.Join(cells[:lastNonEmpty+1], "\t"), nil
		}
	}
}

func parseSpreadsheetCell(decoder *xml.Decoder, start xml.StartElement, sharedStrings []string) (int, string, error) {
	cellType := attrValue(start.Attr, "t")
	cellRef := attrValue(start.Attr, "r")
	columnIndex := spreadsheetColumnIndex(cellRef)
	rawValue := ""
	inlineTexts := make([]string, 0)

	for {
		token, err := decoder.Token()
		if err != nil {
			return 0, "", err
		}

		switch typed := token.(type) {
		case xml.StartElement:
			switch typed.Name.Local {
			case "v":
				var value string
				if err := decoder.DecodeElement(&value, &typed); err != nil {
					return 0, "", err
				}
				rawValue = strings.TrimSpace(value)
			case "t":
				var value string
				if err := decoder.DecodeElement(&value, &typed); err != nil {
					return 0, "", err
				}
				inlineTexts = append(inlineTexts, value)
			}
		case xml.EndElement:
			if typed.Name.Local != "c" {
				continue
			}
			return columnIndex, resolveSpreadsheetCellValue(cellType, rawValue, inlineTexts, sharedStrings), nil
		}
	}
}

func resolveSpreadsheetCellValue(cellType, rawValue string, inlineTexts, sharedStrings []string) string {
	switch cellType {
	case "s":
		index, err := strconv.Atoi(strings.TrimSpace(rawValue))
		if err == nil && index >= 0 && index < len(sharedStrings) {
			return strings.TrimSpace(sharedStrings[index])
		}
		return strings.TrimSpace(rawValue)
	case "inlineStr", "str":
		return normalizeOfficeText(strings.Join(inlineTexts, ""))
	case "b":
		if strings.TrimSpace(rawValue) == "1" {
			return "TRUE"
		}
		if strings.TrimSpace(rawValue) == "0" {
			return "FALSE"
		}
		return strings.TrimSpace(rawValue)
	default:
		if len(inlineTexts) > 0 {
			return normalizeOfficeText(strings.Join(inlineTexts, ""))
		}
		return strings.TrimSpace(rawValue)
	}
}

func spreadsheetColumnIndex(cellRef string) int {
	if cellRef == "" {
		return -1
	}

	cellRef = strings.ToUpper(strings.TrimSpace(cellRef))
	index := 0
	found := false
	for _, ch := range cellRef {
		if ch < 'A' || ch > 'Z' {
			break
		}
		found = true
		index = index*26 + int(ch-'A'+1)
	}
	if !found {
		return -1
	}
	return index - 1
}

func collectZIPFilesByPrefix(files []*zip.File, prefix, suffix string) []*zip.File {
	collected := make([]*zip.File, 0)
	for _, file := range files {
		if file == nil {
			continue
		}
		if strings.HasPrefix(file.Name, prefix) && strings.HasSuffix(file.Name, suffix) {
			collected = append(collected, file)
		}
	}
	sort.Slice(collected, func(i, j int) bool {
		return collected[i].Name < collected[j].Name
	})
	return collected
}

func attrValue(attrs []xml.Attr, local string) string {
	for _, attr := range attrs {
		if attr.Name.Local == local {
			return strings.TrimSpace(attr.Value)
		}
	}
	return ""
}

func readOOXMLRelationships(file *zip.File, baseDir string) (map[string]string, error) {
	result := map[string]string{}
	if file == nil {
		return result, nil
	}

	content, err := readZipFileText(file)
	if err != nil {
		return nil, err
	}

	type relationship struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
	}
	type relationships struct {
		Items []relationship `xml:"Relationship"`
	}

	var parsed relationships
	if err := xml.Unmarshal(content, &parsed); err != nil {
		return nil, err
	}

	for _, item := range parsed.Items {
		target := strings.TrimSpace(item.Target)
		if target == "" {
			continue
		}
		if !strings.HasPrefix(target, "/") {
			target = path.Clean(path.Join(baseDir, target))
		} else {
			target = strings.TrimPrefix(target, "/")
		}
		result[strings.TrimSpace(item.ID)] = target
	}
	return result, nil
}

func collectElementText(decoder *xml.Decoder, endElement string, builder *strings.Builder, preserveTabs bool) error {
	for {
		token, err := decoder.Token()
		if err != nil {
			return err
		}

		switch typed := token.(type) {
		case xml.StartElement:
			switch typed.Name.Local {
			case "t":
				var text string
				if err := decoder.DecodeElement(&text, &typed); err != nil {
					return err
				}
				builder.WriteString(text)
			case "tab":
				if preserveTabs {
					builder.WriteString("\t")
				}
			case "br", "cr":
				builder.WriteString("\n")
			default:
				if err := collectElementText(decoder, typed.Name.Local, builder, preserveTabs); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if typed.Name.Local == endElement {
				return nil
			}
			switch typed.Name.Local {
			case "p", "r", "si":
				builder.WriteString("\n")
			}
		}
	}
}

func normalizeOfficeText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\u00a0", " ")
	lines := strings.Split(value, "\n")
	normalized := make([]string, 0, len(lines))
	blankCount := 0
	for _, line := range lines {
		line = strings.TrimRight(line, "\t ")
		if strings.TrimSpace(line) == "" {
			blankCount++
			if blankCount > 1 {
				continue
			}
			normalized = append(normalized, "")
			continue
		}
		blankCount = 0
		normalized = append(normalized, strings.TrimSpace(line))
	}
	return strings.TrimSpace(strings.Join(normalized, "\n"))
}
