package commandexec

import "testing"

func TestShellCommandUsesConfiguredShellOrder(t *testing.T) {
	fallback := DefaultShell()
	shell, _, err := shellCommand("echo ok", []ShellSpec{{Command: "definitely-not-a-mindfs-shell"}, {Command: fallback}})
	if err != nil {
		t.Fatalf("shellCommand returned error: %v", err)
	}
	if shell != fallback {
		t.Fatalf("shell = %q, want %q", shell, fallback)
	}
}

func TestShellCommandFailsWhenConfiguredShellsAreUnavailable(t *testing.T) {
	_, _, err := shellCommand("echo ok", []ShellSpec{{Command: "definitely-not-a-mindfs-shell"}})
	if err == nil {
		t.Fatalf("shellCommand returned nil error")
	}
}

func TestShellCommandUsesConfiguredArgs(t *testing.T) {
	fallback := DefaultShell()
	shell, args, err := shellCommand("echo ok", []ShellSpec{{Command: fallback, Args: []string{"-custom"}}})
	if err != nil {
		t.Fatalf("shellCommand returned error: %v", err)
	}
	if shell != fallback {
		t.Fatalf("shell = %q, want %q", shell, fallback)
	}
	if len(args) != 2 || args[0] != "-custom" || args[1] != "echo ok" {
		t.Fatalf("args = %#v, want configured args plus command", args)
	}
}

func TestShellCommandUsesConfiguredCommandPrefix(t *testing.T) {
	fallback := DefaultShell()
	_, args, err := shellCommand("echo ok", []ShellSpec{{Command: fallback, Args: []string{"-custom"}, CommandPrefix: "prefix;"}})
	if err != nil {
		t.Fatalf("shellCommand returned error: %v", err)
	}
	if len(args) != 2 || args[1] != "prefix; echo ok" {
		t.Fatalf("args = %#v, want command prefix plus command", args)
	}
}
