package messagecontent

import (
	"encoding/base64"
	"testing"
)

func TestNormalizeSanitizesGenericImageMIMEType(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00}
	parts := Normalize([]Part{{Type: TypeImage, MIMEType: "application/octet-stream", Data: base64.StdEncoding.EncodeToString(png)}})
	if len(parts) != 1 {
		t.Fatalf("expected one normalized part, got %#v", parts)
	}
	if parts[0].MIMEType != "image/png" {
		t.Fatalf("expected image/png, got %#v", parts[0])
	}
}

func TestNormalizeDropsUndetectableGenericImageMIMEType(t *testing.T) {
	parts := Normalize([]Part{{Type: TypeImage, MIMEType: "application/octet-stream", Data: base64.StdEncoding.EncodeToString([]byte("garbage"))}})
	if len(parts) != 0 {
		t.Fatalf("expected undetectable image part to be dropped, got %#v", parts)
	}
}

func TestNormalizeDropsNonImageMimeType(t *testing.T) {
	parts := Normalize([]Part{{Type: TypeImage, MIMEType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("hello"))}})
	if len(parts) != 0 {
		t.Fatalf("expected non-image mime type to be dropped, got %#v", parts)
	}
}

func TestNormalizeSanitizesEmptyMimeTypeFromJPEGHeader(t *testing.T) {
	jpeg := []byte{0xff, 0xd8, 0xff, 0xdb}
	parts := Normalize([]Part{{Type: TypeImage, MIMEType: "", Data: base64.StdEncoding.EncodeToString(jpeg)}})
	if len(parts) != 1 || parts[0].MIMEType != "image/jpeg" {
		t.Fatalf("expected jpeg sniffing, got %#v", parts)
	}
}

func TestNormalizeDropsBadBase64ImagePart(t *testing.T) {
	parts := Normalize([]Part{{Type: TypeImage, MIMEType: "application/octet-stream", Data: "***not-base64***"}})
	if len(parts) != 0 {
		t.Fatalf("expected bad base64 image part to be dropped, got %#v", parts)
	}
}
