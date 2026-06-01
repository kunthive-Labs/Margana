package matrix

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// loginDetails is the set of values needed for an m.login.password flow.
type loginDetails struct {
	homeserver string
	userID     string
	password   string
}

// stdinIsTerminal reports whether stdin is an interactive terminal, so we know
// whether a credential prompt is possible. When false (piped/headless), the
// adapter falls back to MARGA_MATRIX_PASSWORD.
func stdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// readSecret reads a line from the terminal without echoing it, for passwords.
func readSecret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// gatherLogin prompts for any missing connection details and the password. I/O
// is injected so the flow is unit-testable; readSecretFn reads the password
// without echo. Homeserver and user id are only prompted for when empty, so a
// configured [[networks]] entry just asks for the password.
func gatherLogin(out io.Writer, in *bufio.Reader, readSecretFn func(string) (string, error), homeserver, userID string) (loginDetails, error) {
	d := loginDetails{homeserver: homeserver, userID: userID}

	if d.homeserver == "" {
		line, err := promptLine(out, in, "Matrix homeserver URL (e.g. https://matrix.org): ")
		if err != nil {
			return d, err
		}
		d.homeserver = normalizeHomeserver(line)
		if d.homeserver == "" {
			return d, fmt.Errorf("matrix: homeserver is required")
		}
	}
	if d.userID == "" {
		line, err := promptLine(out, in, "Matrix user ID (e.g. @you:matrix.org): ")
		if err != nil {
			return d, err
		}
		d.userID = strings.TrimSpace(line)
		if d.userID == "" {
			return d, fmt.Errorf("matrix: user id is required")
		}
	}

	pw, err := readSecretFn(fmt.Sprintf("Password for %s: ", d.userID))
	if err != nil {
		return d, err
	}
	d.password = pw
	if d.password == "" {
		return d, fmt.Errorf("matrix: password is required")
	}
	return d, nil
}

func promptLine(out io.Writer, in *bufio.Reader, prompt string) (string, error) {
	fmt.Fprint(out, prompt)
	line, err := in.ReadString('\n')
	line = strings.TrimSpace(line)
	// A final line without a trailing newline still yields io.EOF; keep it.
	if err != nil && line == "" {
		return "", err
	}
	return line, nil
}

// normalizeHomeserver defaults a missing scheme to https and trims a trailing
// slash so "matrix.org" and "https://matrix.org/" resolve the same.
func normalizeHomeserver(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		s = "https://" + s
	}
	return strings.TrimSuffix(s, "/")
}
