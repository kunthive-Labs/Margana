package tui

import (
	"hash/fnv"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/kunthive-Labs/Margana/internal/config"
)

// Palette globals, typed as lipgloss.TerminalColor so a theme can supply hex
// colors, ANSI palette indices, or lipgloss.NoColor{} — the latter emits no
// escape, letting a transparent/terminal background show through (see the "none"
// theme and NO_COLOR handling in ApplyTheme). Values here are the built-in
// default; ApplyTheme overwrites them at startup.
var (
	themeBg               lipgloss.TerminalColor = lipgloss.Color("#000000")
	themeFg               lipgloss.TerminalColor = lipgloss.Color("#b3b3b3")
	themeAccent           lipgloss.TerminalColor = lipgloss.Color("#ffffff")
	themeAccentDim        lipgloss.TerminalColor = lipgloss.Color("#666666")
	themeCyan             lipgloss.TerminalColor = lipgloss.Color("#888888")
	themeDim              lipgloss.TerminalColor = lipgloss.Color("#555555")
	themeBorder           lipgloss.TerminalColor = lipgloss.Color("#333333")
	themeStatusBg         lipgloss.TerminalColor = lipgloss.Color("#0a0a0a")
	themeErr              lipgloss.TerminalColor = lipgloss.Color("#ff5454")
	themeWarn             lipgloss.TerminalColor = lipgloss.Color("#ffaa33")
	themeInputBorder      lipgloss.TerminalColor = lipgloss.Color("#444444")
	themeInputBorderFocus lipgloss.TerminalColor = lipgloss.Color("#b0b0b0")
	themeSelectedBg       lipgloss.TerminalColor = lipgloss.Color("#1a1a1a")

	usernameColors = []lipgloss.TerminalColor{
		lipgloss.Color("#ff6b6b"), lipgloss.Color("#ffd93d"), lipgloss.Color("#6bcb77"),
		lipgloss.Color("#4d96ff"), lipgloss.Color("#ff922b"), lipgloss.Color("#cc5de8"),
		lipgloss.Color("#20c997"), lipgloss.Color("#f06595"), lipgloss.Color("#74c0fc"),
		lipgloss.Color("#ff8787"), lipgloss.Color("#a9e34b"), lipgloss.Color("#c0eb75"),
		lipgloss.Color("#ffc9c9"), lipgloss.Color("#91a7ff"), lipgloss.Color("#e599f7"),
	}
)

// Theme is a full palette. Built-ins live in builtinThemes; user palettes from
// config are registered into customThemes via RegisterCustomThemes.
type Theme struct {
	Bg, Fg, Accent, AccentDim, Cyan, Dim, Border, StatusBg,
	Err, Warn, InputBorder, InputBorderFocus, SelectedBg lipgloss.TerminalColor
	UsernameColors []lipgloss.TerminalColor
}

// col and cols build TerminalColors for the theme table.
func col(s string) lipgloss.TerminalColor { return lipgloss.Color(s) }
func cols(vals ...string) []lipgloss.TerminalColor {
	out := make([]lipgloss.TerminalColor, len(vals))
	for i, v := range vals {
		out[i] = lipgloss.Color(v)
	}
	return out
}

var builtinThemes = map[string]Theme{
	"default": {
		Bg: col("#000000"), Fg: col("#b3b3b3"), Accent: col("#ffffff"), AccentDim: col("#666666"),
		Cyan: col("#888888"), Dim: col("#555555"), Border: col("#333333"), StatusBg: col("#0a0a0a"),
		Err: col("#ff5454"), Warn: col("#ffaa33"), InputBorder: col("#444444"),
		InputBorderFocus: col("#b0b0b0"), SelectedBg: col("#1a1a1a"),
		UsernameColors: cols("#ff6b6b", "#ffd93d", "#6bcb77", "#4d96ff", "#ff922b", "#cc5de8",
			"#20c997", "#f06595", "#74c0fc", "#ff8787", "#a9e34b", "#c0eb75", "#ffc9c9",
			"#91a7ff", "#e599f7"),
	},
	"dracula": {
		Bg: col("#282a36"), Fg: col("#f8f8f2"), Accent: col("#bd93f9"), AccentDim: col("#6272a4"),
		Cyan: col("#8be9fd"), Dim: col("#6272a4"), Border: col("#44475a"), StatusBg: col("#21222c"),
		Err: col("#ff5555"), Warn: col("#ffb86c"), InputBorder: col("#44475a"),
		InputBorderFocus: col("#8be9fd"), SelectedBg: col("#44475a"),
		UsernameColors: cols("#ff79c6", "#8be9fd", "#50fa7b", "#bd93f9", "#ffb86c", "#f1fa8c",
			"#ff5555", "#caa9fa", "#7dd3fc", "#f472b6"),
	},
	"solarized": {
		Bg: col("#002b36"), Fg: col("#93a1a1"), Accent: col("#eee8d5"), AccentDim: col("#586e75"),
		Cyan: col("#2aa198"), Dim: col("#657b83"), Border: col("#073642"), StatusBg: col("#073642"),
		Err: col("#dc322f"), Warn: col("#b58900"), InputBorder: col("#586e75"),
		InputBorderFocus: col("#2aa198"), SelectedBg: col("#073642"),
		UsernameColors: cols("#b58900", "#cb4b16", "#dc322f", "#d33682", "#6c71c4", "#268bd2",
			"#2aa198", "#859900", "#839496", "#93a1a1"),
	},
	// "none"/"terminal": inherit the terminal's own background and default
	// foreground (NoColor emits no escape) while using ANSI palette indices for
	// accents so they track the user's 16-color scheme and stay legible on both
	// light and dark backgrounds.
	"none": {
		Bg: lipgloss.NoColor{}, Fg: lipgloss.NoColor{}, StatusBg: lipgloss.NoColor{}, SelectedBg: lipgloss.NoColor{},
		Accent: col("15"), AccentDim: col("8"), Cyan: col("6"), Dim: col("8"), Border: col("8"),
		Err: col("1"), Warn: col("3"), InputBorder: col("8"), InputBorderFocus: col("7"),
		UsernameColors: cols("1", "2", "3", "4", "5", "6", "9", "10", "11", "12", "13", "14"),
	},
}

// customThemes holds palettes loaded from config via RegisterCustomThemes.
var customThemes = map[string]Theme{}

// ApplyTheme selects and applies a palette by name, resolving custom themes
// first, then built-ins, then falling back to "default". "terminal" is an alias
// of "none" (inherit the terminal background so transparent terminals show
// through). When NO_COLOR is set, all color is stripped afterward while text
// attributes (bold, italic) are preserved.
func ApplyTheme(name string) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "terminal" {
		key = "none"
	}
	t, ok := customThemes[key]
	if !ok {
		if t, ok = builtinThemes[key]; !ok {
			t = builtinThemes["default"]
		}
	}
	applyTheme(t)
	if _, noColor := os.LookupEnv("NO_COLOR"); noColor {
		disableColor()
	}
}

func applyTheme(t Theme) {
	themeBg = t.Bg
	themeFg = t.Fg
	themeAccent = t.Accent
	themeAccentDim = t.AccentDim
	themeCyan = t.Cyan
	themeDim = t.Dim
	themeBorder = t.Border
	themeStatusBg = t.StatusBg
	themeErr = t.Err
	themeWarn = t.Warn
	themeInputBorder = t.InputBorder
	themeInputBorderFocus = t.InputBorderFocus
	themeSelectedBg = t.SelectedBg
	if len(t.UsernameColors) > 0 {
		usernameColors = t.UsernameColors
	}
}

// disableColor strips all color (for NO_COLOR); text attributes stay intact.
func disableColor() {
	nc := lipgloss.TerminalColor(lipgloss.NoColor{})
	themeBg, themeFg, themeAccent, themeAccentDim = nc, nc, nc, nc
	themeCyan, themeDim, themeBorder, themeStatusBg = nc, nc, nc, nc
	themeErr, themeWarn, themeInputBorder, themeInputBorderFocus = nc, nc, nc, nc
	themeSelectedBg = nc
	usernameColors = []lipgloss.TerminalColor{nc}
}

// RegisterCustomThemes converts config [themes.<name>] palettes into selectable
// themes. Unspecified fields fall back per-field to the built-in default, so a
// partial palette still works. Call before ApplyTheme.
func RegisterCustomThemes(themes map[string]config.ThemeColors) {
	for name, tc := range themes {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" {
			continue
		}
		customThemes[key] = themeFromColors(tc)
	}
}

func themeFromColors(tc config.ThemeColors) Theme {
	d := builtinThemes["default"]
	pick := func(s string, fallback lipgloss.TerminalColor) lipgloss.TerminalColor {
		if strings.TrimSpace(s) == "" {
			return fallback
		}
		return lipgloss.Color(strings.TrimSpace(s))
	}
	t := Theme{
		Bg:               pick(tc.Bg, d.Bg),
		Fg:               pick(tc.Fg, d.Fg),
		Accent:           pick(tc.Accent, d.Accent),
		AccentDim:        pick(tc.AccentDim, d.AccentDim),
		Cyan:             pick(tc.Cyan, d.Cyan),
		Dim:              pick(tc.Dim, d.Dim),
		Border:           pick(tc.Border, d.Border),
		StatusBg:         pick(tc.StatusBg, d.StatusBg),
		Err:              pick(tc.Err, d.Err),
		Warn:             pick(tc.Warn, d.Warn),
		InputBorder:      pick(tc.InputBorder, d.InputBorder),
		InputBorderFocus: pick(tc.InputBorderFocus, d.InputBorderFocus),
		SelectedBg:       pick(tc.SelectedBg, d.SelectedBg),
		UsernameColors:   d.UsernameColors,
	}
	var uc []lipgloss.TerminalColor
	for _, v := range tc.UsernameColors {
		if strings.TrimSpace(v) != "" {
			uc = append(uc, lipgloss.Color(strings.TrimSpace(v)))
		}
	}
	if len(uc) > 0 {
		t.UsernameColors = uc
	}
	return t
}

func baseStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(themeBg).Foreground(themeFg)
}

func panelStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(themeBorder).
		Background(themeBg).
		Padding(0, 1)
}

func panelTitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(themeAccent).
		Background(themeBg).
		Bold(true).
		Padding(0, 1)
}

func channelStyle(active bool) lipgloss.Style {
	s := lipgloss.NewStyle().
		Foreground(themeAccentDim).
		PaddingLeft(1)
	if active {
		s = s.Background(themeSelectedBg).Foreground(themeAccent).Bold(true)
	}
	return s
}

func userStyle(selected bool) lipgloss.Style {
	s := lipgloss.NewStyle().Foreground(themeAccentDim).PaddingLeft(1)
	if selected {
		s = s.Background(themeSelectedBg).Foreground(themeCyan).Bold(true)
	}
	return s
}

func messageStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(themeFg).PaddingLeft(1)
}

func inputStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(themeInputBorder).
		Background(themeBg).
		Padding(0, 1)
}

func inputFocusedStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(themeInputBorderFocus).
		Background(themeBg).
		Padding(0, 1)
}

func statusBarStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(themeStatusBg).
		Foreground(themeAccent).
		Padding(0, 1)
}

func statusErrorStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(themeStatusBg).
		Foreground(themeErr).
		Padding(0, 1)
}

func loadingStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(themeDim).
		Italic(true)
}

func promptStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(themeAccent).
		Bold(true)
}

func usernameColor(name string) lipgloss.TerminalColor {
	h := fnv.New32a()
	h.Write([]byte(name))
	return usernameColors[int(h.Sum32())%len(usernameColors)]
}

func systemMessageStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(themeDim).
		Italic(true).
		PaddingLeft(1)
}

func coloredUsername(name string) string {
	return lipgloss.NewStyle().Foreground(usernameColor(name)).Render(name)
}

func typingStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(themeDim).
		Italic(true).
		PaddingLeft(1)
}

func commandSuggestionStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(themeFg).
		Background(themeStatusBg).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(themeBorder).
		Padding(0, 1)
}

func presenceStatusStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(themeDim).
		Italic(true).
		PaddingLeft(2)
}

func newMsgBannerStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(themeAccent).
		Background(themeSelectedBg).
		Bold(true)
}

func replyPreviewStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(themeDim).
		Italic(true).
		PaddingLeft(3).
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(themeAccentDim)
}

func replyBarStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(themeWarn).
		Background(themeStatusBg).
		Bold(false).
		Padding(0, 1)
}

func notifItemStyle(selected bool) lipgloss.Style {
	s := lipgloss.NewStyle().Foreground(themeFg).PaddingLeft(1)
	if selected {
		s = s.Background(themeSelectedBg).Foreground(themeAccent).Bold(true)
	}
	return s
}

func mentionBadgeStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(themeStatusBg).
		Background(themeWarn).
		Bold(true).
		Padding(0, 1)
}

func replySelectStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(themeSelectedBg).
		BorderLeft(true).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(themeAccent).
		PaddingLeft(1)
}

func replySelectPromptStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(themeAccent).
		Background(themeStatusBg).
		Bold(true).
		Padding(0, 1)
}

func autoCompleteStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(themeDim).
		Background(themeStatusBg).
		Padding(0, 1)
}

func autoCompleteItemStyle(highlighted bool) lipgloss.Style {
	s := lipgloss.NewStyle().Foreground(themeAccentDim)
	if highlighted {
		s = s.Foreground(themeAccent).Bold(true)
	}
	return s
}
