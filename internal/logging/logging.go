// Package logging provides Marga's application logging: a leveled, structured
// logger built on the standard library's log/slog that writes to a file rather
// than the terminal.
//
// Marga is a full-screen TUI. Writing logs to stdout or stderr would corrupt
// the rendered interface, so logging always targets a file — or is disabled
// entirely. It is off unless the operator opts in (via the --debug / --log-file
// flags or the [logging] config section), which keeps the default experience
// silent while making real diagnostics available in production.
//
// The package is intentionally dependency-free (standard library only). It
// bridges into two other logging styles used by the codebase:
//
//   - StdLogger returns a *log.Logger for packages that take the standard
//     library logger (the WebSocket client and the TUI).
//   - Writer exposes the underlying file so callers can point a third-party
//     logger (e.g. mautrix's zerolog) at the same destination.
package logging

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Format selects how log records are encoded.
type Format string

const (
	// FormatText writes human-readable key=value lines (the default).
	FormatText Format = "text"
	// FormatJSON writes one JSON object per record, for ingestion by log
	// processors.
	FormatJSON Format = "json"
)

// Options configures a Logger built by New.
type Options struct {
	// Level is the minimum severity that will be written.
	Level slog.Level
	// File is the destination path. When empty, logging is disabled and New
	// returns a no-op Logger that opens no file.
	File string
	// Format selects text (default) or JSON encoding.
	Format Format
	// AddSource includes the source file:line of each call site. Helpful when
	// debugging, noisier in normal use.
	AddSource bool
}

// Logger is Marga's application logger. The embedded *slog.Logger provides the
// structured logging API (Debug/Info/Warn/Error, With, …). Build one with New
// or Disabled; the zero value is not usable.
type Logger struct {
	*slog.Logger
	writer  io.Writer
	closer  io.Closer
	level   slog.Level
	enabled bool
}

// Disabled returns a Logger that discards everything. Every method on it is
// safe to call; nothing is written and no file is opened. It is the default
// state when the operator has not enabled logging.
func Disabled() *Logger {
	return &Logger{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		writer:  io.Discard,
		level:   slog.LevelInfo,
		enabled: false,
	}
}

// New builds a Logger from opts. When opts.File is empty it returns a disabled
// (no-op) Logger and no error, so callers can wire logging unconditionally and
// let configuration decide whether anything is written.
//
// Otherwise it creates the log file's parent directory and opens the file for
// appending. The caller owns the returned Logger and must Close it to flush and
// release the file handle.
func New(opts Options) (*Logger, error) {
	if strings.TrimSpace(opts.File) == "" {
		return Disabled(), nil
	}

	if dir := filepath.Dir(opts.File); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("logging: creating log directory: %w", err)
		}
	}

	// 0600: log files can contain operationally sensitive detail; keep them
	// readable only by the owner.
	f, err := os.OpenFile(opts.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("logging: opening log file %s: %w", opts.File, err)
	}

	handlerOpts := &slog.HandlerOptions{Level: opts.Level, AddSource: opts.AddSource}
	var handler slog.Handler
	switch opts.Format {
	case FormatJSON:
		handler = slog.NewJSONHandler(f, handlerOpts)
	default:
		handler = slog.NewTextHandler(f, handlerOpts)
	}

	return &Logger{
		Logger:  slog.New(handler),
		writer:  f,
		closer:  f,
		level:   opts.Level,
		enabled: true,
	}, nil
}

// Enabled reports whether the logger writes anywhere. It is false for a
// disabled logger.
func (l *Logger) Enabled() bool { return l != nil && l.enabled }

// Level returns the configured minimum level.
func (l *Logger) Level() slog.Level { return l.level }

// Writer returns the underlying log destination, so a third-party logger (for
// example mautrix's zerolog) can be pointed at the same file. It returns
// io.Discard when logging is disabled.
func (l *Logger) Writer() io.Writer {
	if l == nil {
		return io.Discard
	}
	return l.writer
}

// Close flushes and closes the underlying file, if any. It is safe to call on a
// disabled logger.
func (l *Logger) Close() error {
	if l == nil || l.closer == nil {
		return nil
	}
	return l.closer.Close()
}

// Named returns a child slog.Logger tagged with a "component" attribute, so log
// lines can be attributed to a subsystem.
func (l *Logger) Named(component string) *slog.Logger {
	if l == nil {
		return Disabled().Logger
	}
	return l.With("component", component)
}

// StdLogger returns a *log.Logger that forwards every line it receives to this
// logger's handler at the given level, tagged with the component attribute.
// This bridges packages that take the standard library logger (the WebSocket
// client, the TUI) into the structured log file. When logging is disabled it
// returns a logger writing to io.Discard, preserving the silent default.
func (l *Logger) StdLogger(component string, level slog.Level) *log.Logger {
	if !l.Enabled() {
		return log.New(io.Discard, "", 0)
	}
	h := l.Handler()
	if component != "" {
		h = h.WithAttrs([]slog.Attr{slog.String("component", component)})
	}
	return slog.NewLogLogger(h, level)
}

// Settings are the logging values sourced from configuration (the [logging]
// TOML section and MARGA_LOG_* environment overrides).
type Settings struct {
	Level  string
	File   string
	Format string
}

// Flags are the logging-related command-line overrides.
type Flags struct {
	// Debug forces debug level and, when no file is configured anywhere,
	// enables logging at the default path — the one-flag "turn it on" switch.
	Debug bool
	// File is an explicit --log-file destination.
	File string
	// Level is an explicit --log-level name.
	Level string
}

// Resolve combines configuration with command-line overrides into Options,
// applying Marga's precedence: explicit flags win over the config file and
// environment. --debug forces debug level and, when nothing else set a file,
// enables logging at defaultFile so a single flag is enough to capture logs.
//
// When no destination is set by config or flags, the returned Options.File is
// empty and New will produce a disabled logger — logging stays off until the
// operator opts in.
func Resolve(s Settings, f Flags, defaultFile string) Options {
	opts := Options{
		Level:  ParseLevel(s.Level),
		File:   strings.TrimSpace(s.File),
		Format: ParseFormat(s.Format),
	}
	if f.Level != "" {
		opts.Level = ParseLevel(f.Level)
	}
	if strings.TrimSpace(f.File) != "" {
		opts.File = strings.TrimSpace(f.File)
	}
	if f.Debug {
		opts.Level = slog.LevelDebug
		if opts.File == "" {
			opts.File = defaultFile
		}
	}
	return opts
}

// ParseLevel maps a level name (debug/info/warn/error, case-insensitive) to a
// slog.Level. Empty or unrecognized values default to Info.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "err":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// ParseFormat maps a format name to a Format. Empty or unrecognized values
// default to text.
func ParseFormat(s string) Format {
	if strings.EqualFold(strings.TrimSpace(s), string(FormatJSON)) {
		return FormatJSON
	}
	return FormatText
}
