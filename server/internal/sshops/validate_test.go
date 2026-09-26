package sshops

import (
	"strings"
	"testing"
)

func TestValidateAliasRules(t *testing.T) {
	bad := []string{
		"", "  ", "has space", "has\ttab", "line\nbreak", "semi;colon",
		"=injected", "#comment", "-leading",
		strings.Repeat("a", 65), "*", "ssh", "match",
	}
	for _, alias := range bad {
		s := Server{Alias: alias, Host: "10.0.0.1", Port: 22, User: "root", Auth: AuthPassword}
		if err := Validate(s); err == nil {
			t.Fatalf("alias %q should be rejected", alias)
		}
	}
	good := []string{"crunchbits", "web-1", "db.primary", "node_02", "a", "dash-", "UPPER_ok_but_l", strings.Repeat("a", 64)}
	for _, alias := range good {
		s := Server{Alias: alias, Host: "10.0.0.1", Port: 22, User: "root", Auth: AuthPassword}
		if err := Validate(s); err != nil {
			t.Fatalf("alias %q should pass: %v", alias, err)
		}
	}
}

func TestValidateHostRules(t *testing.T) {
	bad := []string{
		"", "host with space", "a#b", "a=b", "line\ninjected", "a\tb",
	}
	for _, host := range bad {
		s := Server{Alias: "ok", Host: host, Port: 22, User: "root", Auth: AuthPassword}
		if err := Validate(s); err == nil {
			t.Fatalf("host %q should be rejected", host)
		}
	}
	good := []string{"203.0.113.10", "example.com", "infra-01.internal", "::1", "[2001:db8::1]", "vpn.lan"}
	for _, host := range good {
		s := Server{Alias: "ok", Host: host, Port: 22, User: "root", Auth: AuthPassword}
		if err := Validate(s); err != nil {
			t.Fatalf("host %q should pass: %v", host, err)
		}
	}
}

func TestValidatePortUserAuth(t *testing.T) {
	base := func() Server {
		return Server{Alias: "ok", Host: "10.0.0.1", User: "root", Auth: AuthPassword}
	}
	if err := Validate(base()); err != nil {
		t.Fatalf("default port should pass: %v", err)
		s := base()
		s.Port = 22
		_ = s
	}
	cases := []struct {
		mutate func(*Server)
	}{
		{func(s *Server) { s.Port = -1 }},
		{func(s *Server) { s.Port = 65536 }},
		{func(s *Server) { s.User = "bad user" }},
		{func(s *Server) { s.User = "" }},
		{func(s *Server) { s.User = "1leadingdigit" }},
		{func(s *Server) { s.Auth = AuthMode("nope") }},
		{func(s *Server) { s.ProxyJump = "a b" }},
		{func(s *Server) { s.Notes = strings.Repeat("n", 501) }},
	}
	for i, c := range cases {
		s := base()
		s.Port = 22
		c.mutate(&s)
		if err := Validate(s); err == nil {
			t.Fatalf("case %d should fail", i)
		}
	}
}

func TestValidateKeyPath(t *testing.T) {
	good := []string{"/home/me/.ssh/id_ed25519", "~/.ssh/id_rsa", `C:\Users\me\.ssh\id_ed25519`, "D:/keys/k"}
	for _, p := range good {
		if err := ValidateKeyPath(p); err != nil {
			t.Fatalf("%q should pass: %v", p, err)
		}
	}
	bad := []string{"", "relative/path", "path with newline\nHost evil", "~other/x"}
	for _, p := range bad {
		if err := ValidateKeyPath(p); err == nil {
			t.Fatalf("%q should fail", p)
		}
	}
}
