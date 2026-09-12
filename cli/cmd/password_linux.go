//go:build linux

package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// readSecret prompts for a password on stderr and disables terminal echo while
// reading. When stdin is not a terminal it falls back to a plain read so the
// command can be scripted with a pipe.
func readSecret(reader *bufio.Reader, prompt string) string {
	fmt.Fprint(os.Stderr, prompt)
	fd := int(os.Stdin.Fd())
	oldState, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		fmt.Fprintln(os.Stderr)
		return readLine(reader)
	}
	newState := *oldState
	newState.Lflag &^= unix.ECHO
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &newState); err != nil {
		fmt.Fprintln(os.Stderr)
		return readLine(reader)
	}
	line := readLine(reader)
	_ = unix.IoctlSetTermios(fd, unix.TCSETS, oldState)
	fmt.Fprintln(os.Stderr)
	return line
}

func readLine(reader *bufio.Reader) string {
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return ""
	}
	return strings.TrimRight(line, "\r\n")
}
