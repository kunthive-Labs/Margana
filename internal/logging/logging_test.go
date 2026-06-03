package logging

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDisabledLoggerWritesNothing(t *testing.T) {
	l := Disabled()
	if l.Enabled() {
		t.Fatal("Disabled() logger should report Enabled() == false")
	}
	// Must not panic and must be a no-op.
	l.Info("hello", "k", "v")
	l.StdLogger("ws", slog.LevelInfo).Printf("nothing here")
	if err := l.Close(); err != nil {
		t.Fatalf("Close on disabled logger: %v", err)
	}
}

func TestNewEmptyFileReturnsDisabled(t *testing.T) {
	l, err := New(Options{File: "  "})
	if err != nil {
		t.Fatalf("New with blank file: %v", err)
	}
	if l.Enabled() {
		t.Fatal("blank file should yield a disabled logger")
	}
}

func TestNewWritesLeveledRecordsToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "marga.log") // sub dir must be created
	l, err := New(Options{File: path, Level: slog.LevelInfo})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	l.Debug("should-not-appear")
	l.Info("should-appear", "answer", 42)
	l.Warn("warned")

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	out := string(data)
	if strings.Contains(out, "should-not-appear") {
		t.Errorf("debug record written despite Info level:\n%s", out)
	}
	if !strings.Contains(out, "should-appear") || !strings.Contains(out, "answer=42") {
		t.Errorf("info record missing or malformed:\n%s", out)
	}
	if !strings.Contains(out, "warned") {
		t.Errorf("warn record missing:\n%s", out)
	}
}

func TestNewJSONFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "marga.log")
	l, err := New(Options{File: path, Level: slog.LevelDebug, Format: FormatJSON})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	l.Info("json-line", "k", "v")
	l.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	line := strings.TrimSpace(string(data))
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("log line is not JSON: %v\n%s", err, line)
	}
	if rec["msg"] != "json-line" || rec["k"] != "v" {
		t.Errorf("unexpected JSON record: %v", rec)
	}
}

func TestStdLoggerBridgeTagsComponent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "marga.log")
	l, err := New(Options{File: path, Level: slog.LevelInfo})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	std := l.StdLogger("ws", slog.LevelInfo)
	std.Printf("ws: connection lost")
	l.Close()

	data, _ := os.ReadFile(path)
	out := string(data)
	if !strings.Contains(out, "ws: connection lost") {
		t.Errorf("bridged line missing:\n%s", out)
	}
	if !strings.Contains(out, "component=ws") {
		t.Errorf("component attribute missing:\n%s", out)
	}
}

func TestStdLoggerBelowMinLevelIsDropped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "marga.log")
	l, _ := New(Options{File: path, Level: slog.LevelError})
	// Bridge emits at Info, but the handler only keeps Error and above.
	l.StdLogger("tui", slog.LevelInfo).Printf("noise")
	l.Close()

	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "noise") {
		t.Errorf("info-level bridge line should be dropped at Error min level:\n%s", string(data))
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"INFO":    slog.LevelInfo,
		"Warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"":        slog.LevelInfo,
		"bogus":   slog.LevelInfo,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseFormat(t *testing.T) {
	if ParseFormat("json") != FormatJSON {
		t.Error("json should parse to FormatJSON")
	}
	if ParseFormat("JSON") != FormatJSON {
		t.Error("JSON should be case-insensitive")
	}
	if ParseFormat("text") != FormatText || ParseFormat("") != FormatText || ParseFormat("xml") != FormatText {
		t.Error("non-json should default to FormatText")
	}
}

func TestResolvePrecedence(t *testing.T) {
	const def = "/var/marga/marga.log"

	t.Run("nothing set -> disabled", func(t *testing.T) {
		o := Resolve(Settings{}, Flags{}, def)
		if o.File != "" {
			t.Errorf("expected empty file (disabled), got %q", o.File)
		}
		if o.Level != slog.LevelInfo {
			t.Errorf("expected default Info level, got %v", o.Level)
		}
	})

	t.Run("config file enables at info", func(t *testing.T) {
		o := Resolve(Settings{File: "/cfg.log", Level: "warn", Format: "json"}, Flags{}, def)
		if o.File != "/cfg.log" || o.Level != slog.LevelWarn || o.Format != FormatJSON {
			t.Errorf("config not honored: %+v", o)
		}
	})

	t.Run("debug forces level and default file", func(t *testing.T) {
		o := Resolve(Settings{}, Flags{Debug: true}, def)
		if o.File != def || o.Level != slog.LevelDebug {
			t.Errorf("--debug should enable default file at debug: %+v", o)
		}
	})

	t.Run("flags override config", func(t *testing.T) {
		o := Resolve(
			Settings{File: "/cfg.log", Level: "info"},
			Flags{File: "/flag.log", Level: "error"},
			def,
		)
		if o.File != "/flag.log" || o.Level != slog.LevelError {
			t.Errorf("flags should win over config: %+v", o)
		}
	})

	t.Run("debug keeps explicit flag file", func(t *testing.T) {
		o := Resolve(Settings{}, Flags{Debug: true, File: "/explicit.log"}, def)
		if o.File != "/explicit.log" || o.Level != slog.LevelDebug {
			t.Errorf("--debug with --log-file should keep the explicit file: %+v", o)
		}
	})
}

func TestWriterIsUsableForBridging(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "marga.log")
	l, _ := New(Options{File: path, Level: slog.LevelInfo})
	defer l.Close()
	if _, err := l.Writer().Write([]byte("raw-bridge-line\n")); err != nil {
		t.Fatalf("writing to Writer(): %v", err)
	}
	l.Close()
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "raw-bridge-line") {
		t.Errorf("Writer() did not reach the file:\n%s", string(data))
	}
}
