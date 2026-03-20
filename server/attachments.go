package main

import (
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
)

type botAttachment struct {
	FileID    string
	Name      string
	MIMEType  string
	Extension string
	Size      int64
	Content   []byte
}

func (p *Plugin) collectBotAttachments(fileIDs []string, channelID string) ([]botAttachment, error) {
	if len(fileIDs) == 0 {
		return nil, nil
	}

	attachments := make([]botAttachment, 0, len(fileIDs))
	for _, fileID := range fileIDs {
		fileID = strings.TrimSpace(fileID)
		if fileID == "" {
			continue
		}

		info, appErr := p.API.GetFileInfo(fileID)
		if appErr != nil {
			return nil, fmt.Errorf("failed to load Mattermost file info %q: %w", fileID, appErr)
		}
		if strings.TrimSpace(channelID) != "" && strings.TrimSpace(info.ChannelId) != "" && info.ChannelId != channelID {
			return nil, fmt.Errorf("Mattermost file %q does not belong to channel %q", attachmentLabel(info), channelID)
		}

		content, appErr := p.API.GetFile(fileID)
		if appErr != nil {
			return nil, fmt.Errorf("failed to download Mattermost file %q: %w", fileID, appErr)
		}

		attachments = append(attachments, botAttachment{
			FileID:    fileID,
			Name:      defaultIfEmpty(strings.TrimSpace(info.Name), fileID),
			MIMEType:  detectAttachmentMIMEType(info, content),
			Extension: detectAttachmentExtension(info),
			Size:      info.Size,
			Content:   content,
		})
	}

	return attachments, nil
}

func detectAttachmentMIMEType(info *model.FileInfo, content []byte) string {
	if info != nil {
		if value := normalizeAttachmentMIMEType(info.MimeType); value != "" &&
			value != "application/octet-stream" &&
			value != "binary/octet-stream" &&
			value != "application/zip" {
			return value
		}
		if extension := detectAttachmentExtension(info); extension != "" {
			if detected := mime.TypeByExtension("." + extension); detected != "" {
				return normalizeAttachmentMIMEType(detected)
			}
		}
	}

	if len(content) > 0 {
		if detected := normalizeAttachmentMIMEType(http.DetectContentType(content)); detected != "" {
			return detected
		}
	}

	if info != nil {
		if value := normalizeAttachmentMIMEType(info.MimeType); value != "" {
			return value
		}
	}

	return "application/octet-stream"
}

func detectAttachmentExtension(info *model.FileInfo) string {
	if info == nil {
		return ""
	}

	for _, candidate := range []string{
		strings.TrimSpace(info.Extension),
		strings.TrimPrefix(filepath.Ext(strings.TrimSpace(info.Name)), "."),
	} {
		candidate = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(candidate), "."))
		if candidate != "" {
			return candidate
		}
	}

	mimeType := normalizeAttachmentMIMEType(info.MimeType)
	if mimeType == "" {
		return ""
	}

	extensions, err := mime.ExtensionsByType(mimeType)
	if err != nil {
		return ""
	}
	for _, extension := range extensions {
		extension = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(extension), "."))
		if extension != "" {
			return extension
		}
	}

	return ""
}

func normalizeAttachmentMIMEType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}

	if index := strings.Index(value, ";"); index >= 0 {
		value = strings.TrimSpace(value[:index])
	}

	return value
}

func attachmentLabel(info *model.FileInfo) string {
	if info == nil {
		return ""
	}
	if value := strings.TrimSpace(info.Name); value != "" {
		return value
	}
	return strings.TrimSpace(info.Id)
}

func sanitizeUploadFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "attachment"
	}
	base := filepath.Base(name)
	if strings.TrimSpace(base) == "" || base == "." || base == string(filepath.Separator) {
		return "attachment"
	}
	return base
}
