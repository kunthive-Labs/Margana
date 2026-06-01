package matrix

import (
	"testing"

	"maunium.net/go/mautrix/event"
)

func TestMsgTypeForFile(t *testing.T) {
	cases := []struct {
		name string
		want event.MessageType
	}{
		{"photo.png", event.MsgImage},
		{"photo.JPG", event.MsgImage},
		{"clip.mp4", event.MsgVideo},
		{"song.mp3", event.MsgAudio},
		{"notes.txt", event.MsgFile},
		{"archive.tar.gz", event.MsgFile},
		{"noext", event.MsgFile},
	}
	for _, c := range cases {
		if got := msgTypeForFile(c.name); got != c.want {
			t.Errorf("msgTypeForFile(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDetectContentType(t *testing.T) {
	if got := detectContentType("a.png"); got != "image/png" {
		t.Errorf("png content type = %q", got)
	}
	if got := detectContentType("unknown.zzz"); got != "application/octet-stream" {
		t.Errorf("unknown extension should fall back to octet-stream, got %q", got)
	}
}

func TestFilepathBase(t *testing.T) {
	if got := filepathBase("/tmp/sub/file.txt"); got != "file.txt" {
		t.Errorf("filepathBase = %q", got)
	}
}

func TestMediaAttachment(t *testing.T) {
	content := &event.MessageEventContent{
		MsgType:  event.MsgImage,
		FileName: "pic.png",
		URL:      "mxc://hs/abc",
		Info:     &event.FileInfo{MimeType: "image/png", Width: 64, Height: 48, Size: 999},
	}
	att := mediaAttachment(content)
	if att == nil {
		t.Fatal("expected attachment for image message")
	}
	if att.Filename != "pic.png" || att.URL != "mxc://hs/abc" || att.ContentType != "image/png" {
		t.Errorf("bad attachment: %+v", att)
	}
	if att.Width != 64 || att.Height != 48 || att.Size != 999 {
		t.Errorf("bad attachment dims: %+v", att)
	}
}

func TestMediaAttachmentFallsBackToBody(t *testing.T) {
	content := &event.MessageEventContent{MsgType: event.MsgFile, Body: "report.pdf", URL: "mxc://hs/x"}
	att := mediaAttachment(content)
	if att == nil || att.Filename != "report.pdf" {
		t.Errorf("expected filename to fall back to body, got %+v", att)
	}
}

func TestMediaAttachmentNilForText(t *testing.T) {
	if att := mediaAttachment(&event.MessageEventContent{MsgType: event.MsgText, Body: "hi"}); att != nil {
		t.Errorf("text message should have no attachment, got %+v", att)
	}
}
