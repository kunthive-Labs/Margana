package setup

import "fmt"

// ANSI color codes — minimal palette: cyan accent, dim gray, white, green, yellow, red
const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
	colorItalic = "\033[3m"

	colorWhite  = "\033[97m"
	colorCyan   = "\033[36m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorGray   = "\033[90m"
)

// styled helpers — keep output compact and consistent
func accent(s string) string  { return colorCyan + s + colorReset }
func bold(s string) string    { return colorBold + colorWhite + s + colorReset }
func dim(s string) string     { return colorDim + s + colorReset }
func success(s string) string { return colorGreen + s + colorReset }
func warn(s string) string    { return colorYellow + s + colorReset }
func errText(s string) string { return colorRed + s + colorReset }
func italic(s string) string  { return colorItalic + colorGray + s + colorReset }

// clearScreen clears the terminal
func clearScreen() {
	fmt.Print("\033[2J\033[H")
}

func printLogo() {
	fmt.Println()
	logo := []string{
		"                       888 888         ",
		"                       888 888         ",
		"                       888 888         ",
		"88888b.d88b.  .d88b.  888 888 888  888",
		"888 \"888 \"88b d88\"\"88b 888 888 888  888",
		"888  888  888 888  888 888 888 888  888",
		"888  888  888 Y88..88P 888 888 Y88b 888",
		"888  888  888  \"Y88P\"  888 888  \"Y88888",
		"                                    888",
		"                               Y8b d88P",
		"                                \"Y88P\" ",
	}
	for i, line := range logo {
		if i == 5 {
			fmt.Println("  " + accent("> ") + bold(line))
		} else {
			fmt.Println("    " + bold(line))
		}
	}
	fmt.Println()
}

// bullet prints a single compact line with a leading dot
func bullet(label, value string) {
	fmt.Printf("  %s %s %s\n", dim("•"), dim(label), value)
}

// step prints a step indicator like "▸ Step description"
func step(s string) {
	fmt.Printf("  %s %s\n", accent("▸"), s)
}

// option prints a numbered/keyed option: "  [key] description"
func option(key, desc string) {
	fmt.Printf("    %s  %s\n", accent("["+key+"]"), desc)
}

// prompt prints an inline prompt and stays on the same line
func prompt(s string) {
	fmt.Printf("  %s %s", accent("›"), s)
}
