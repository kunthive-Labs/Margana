package matrix

import (
	"mime"
	"path/filepath"
	"strings"

	"maunium.net/go/mautrix/event"
)

func filepathBase(path string) string { return filepath.Base(path) }

func detectContentType(name string) string {
	if ct := mime.TypeByExtension(filepath.Ext(name)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

// msgTypeForFile picks the Matrix msgtype from a filename's extension.
func msgTypeForFile(name string) event.MessageType {
	ct := detectContentType(name)
	switch {
	case strings.HasPrefix(ct, "image/"):
		return event.MsgImage
	case strings.HasPrefix(ct, "video/"):
		return event.MsgVideo
	case strings.HasPrefix(ct, "audio/"):
		return event.MsgAudio
	default:
		return event.MsgFile
	}
}
