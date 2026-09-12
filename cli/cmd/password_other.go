//go:build !linux

package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// readSecret prompts for a password and reads it from stdin. Terminal echo is
// not suppressed on this platform; pipe the value in for scripted use.
func readSecret(reader *bufio.Reader, prompt string) string {
	fmt.Fprint(os.Stderr, prompt)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return ""
	}
	return strings.TrimRight(line, "\r\n")
}

func readLine(reader *bufio.Reader) string {
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return ""
	}
	return strings.TrimRight(line, "\r\n")
}
