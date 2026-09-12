package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"mindfs/server/auth"
)

// handleAuthCommand implements the `mindfs auth ...` subcommands. Only
// `set-password` is supported; enabling/disabling and changing the username are
// done from the web UI.
func handleAuthCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mindfs auth set-password [-username <name>] [-password <password>]")
	}
	switch strings.TrimSpace(args[0]) {
	case "set-password":
		return handleAuthSetPassword(args[1:])
	default:
		return fmt.Errorf("unknown auth subcommand %q", strings.TrimSpace(args[0]))
	}
}

func handleAuthSetPassword(args []string) error {
	fs := flag.NewFlagSet("auth set-password", flag.ContinueOnError)
	username := fs.String("username", "", "login username; prompted when empty")
	password := fs.String("password", "", "login password; prompted when empty")
	if err := fs.Parse(args); err != nil {
		return err
	}

	mgr, err := auth.NewManager()
	if err != nil {
		return err
	}

	reader := bufio.NewReader(os.Stdin)
	name := strings.TrimSpace(*username)
	if name == "" {
		existing := strings.TrimSpace(mgr.Status().Username)
		prompt := "Username: "
		if existing != "" {
			prompt = fmt.Sprintf("Username [%s]: ", existing)
		}
		name = promptLine(reader, prompt)
		if name == "" {
			name = existing
		}
	}
	if name == "" {
		return fmt.Errorf("username is required")
	}

	pass := *password
	fromFlag := strings.TrimSpace(pass) != ""
	if !fromFlag {
		pass = readSecret(reader, "Password: ")
	}
	if strings.TrimSpace(pass) == "" {
		return fmt.Errorf("password is required")
	}
	if !fromFlag {
		confirm := readSecret(reader, "Confirm password: ")
		if pass != confirm {
			return fmt.Errorf("passwords do not match")
		}
	}

	if err := mgr.SetCredentials(name, pass); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Password updated for %q.\n", name)
	if !mgr.Enabled() {
		fmt.Fprintln(os.Stdout, "Login is currently disabled. Enable it from the web UI (top-right account menu -> Auth settings).")
	}
	return nil
}

func promptLine(reader *bufio.Reader, prompt string) string {
	fmt.Fprint(os.Stderr, prompt)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return ""
	}
	return strings.TrimSpace(line)
}
