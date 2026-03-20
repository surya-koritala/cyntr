package agent

import "testing"

func TestAttachmentType(t *testing.T) {
	a := Attachment{Type: "image", URL: "https://example.com/img.png", MimeType: "image/png", Name: "img.png"}
	if a.Type != "image" {
		t.Fatalf("got %q", a.Type)
	}
	if a.MimeType != "image/png" {
		t.Fatalf("got %q", a.MimeType)
	}
}

func TestMessageWithAttachments(t *testing.T) {
	msg := Message{
		Role: RoleUser, Content: "What's in this image?",
		Attachments: []Attachment{{Type: "image", URL: "data:image/png;base64,abc123", MimeType: "image/png"}},
	}
	if len(msg.Attachments) != 1 {
		t.Fatal("expected 1 attachment")
	}
}

func TestMessageWithoutAttachments(t *testing.T) {
	msg := Message{Role: RoleUser, Content: "Hello"}
	if len(msg.Attachments) != 0 {
		t.Fatal("expected no attachments")
	}
}
