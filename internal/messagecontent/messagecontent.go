package messagecontent

import (
	"encoding/base64"
	"strings"
)

const (
	TypeText  = "text"
	TypeImage = "image"
)

var allowedImageMIMETypes = map[string]struct{}{
	"image/gif":  {},
	"image/jpeg": {},
	"image/png":  {},
	"image/webp": {},
}

type Part struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`
	Data     string `json:"data,omitempty"`
}

func Normalize(parts []Part) []Part {
	if len(parts) == 0 {
		return nil
	}
	out := make([]Part, 0, len(parts))
	for _, part := range parts {
		switch strings.ToLower(strings.TrimSpace(part.Type)) {
		case TypeText:
			text := strings.TrimSpace(part.Text)
			if text == "" {
				continue
			}
			out = append(out, Part{Type: TypeText, Text: text})
		case TypeImage:
			mimeType := sanitizeImageMIMEType(part.MIMEType, part.Data)
			data := strings.TrimSpace(part.Data)
			if mimeType == "" || data == "" {
				continue
			}
			out = append(out, Part{Type: TypeImage, MIMEType: mimeType, Data: data})
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sanitizeImageMIMEType(mimeType, data string) string {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	if _, ok := allowedImageMIMETypes[mimeType]; ok {
		return mimeType
	}
	body, err := decodeImageHeaderPrefix(data, 16)
	if err != nil || len(body) == 0 {
		return ""
	}
	if len(body) >= 8 && string(body[:8]) == string([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return "image/png"
	}
	if len(body) >= 12 && string(body[:4]) == "RIFF" && string(body[8:12]) == "WEBP" {
		return "image/webp"
	}
	if len(body) >= 3 && body[0] == 0xff && body[1] == 0xd8 && body[2] == 0xff {
		return "image/jpeg"
	}
	if len(body) >= 6 {
		header := string(body[:6])
		if header == "GIF87a" || header == "GIF89a" {
			return "image/gif"
		}
	}
	return ""
}

func decodeImageHeaderPrefix(data string, maxBytes int) ([]byte, error) {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" || maxBytes <= 0 {
		return nil, nil
	}
	encodedLen := base64.StdEncoding.EncodedLen(maxBytes)
	if encodedLen > len(trimmed) {
		encodedLen = len(trimmed)
	} else {
		encodedLen -= encodedLen % 4
		if encodedLen <= 0 {
			encodedLen = len(trimmed) - (len(trimmed) % 4)
		}
	}
	if encodedLen <= 0 {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(trimmed[:encodedLen])
}

func VisibleText(parts []Part) string {
	if len(parts) == 0 {
		return ""
	}
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.EqualFold(strings.TrimSpace(part.Type), TypeText) {
			text := strings.TrimSpace(part.Text)
			if text != "" {
				texts = append(texts, text)
			}
		}
	}
	return strings.TrimSpace(strings.Join(texts, "\n"))
}

func HasImage(parts []Part) bool {
	for _, part := range parts {
		if strings.EqualFold(strings.TrimSpace(part.Type), TypeImage) {
			return true
		}
	}
	return false
}
