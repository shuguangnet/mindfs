package sshops

import (
	"regexp"
	"strings"
)

func mustPattern(expr string) *regexp.Regexp {
	return regexp.MustCompile(expr)
}

var (
	aliasPattern = mustPattern(`^[a-z0-9][a-z0-9_.-]{0,63}$`)
	hostPattern  = mustPattern(`^[A-Za-z0-9._:\[\]-]{1,255}$`)
	userPattern  = mustPattern(`^[A-Za-z_][A-Za-z0-9._-]{0,31}$`)
	proxyPattern = mustPattern(`^[A-Za-z0-9][A-Za-z0-9_.@:-]{0,127}$`)
)

// reservedAliases are values that would be ambiguous or dangerous as Host
// patterns in the generated OpenSSH configuration.
var reservedAliases = map[string]bool{
	"*": true, "all": true, "ssh": true, "host": true, "match": true,
	"include": true, "config": true, "addkeystoagent": true, "identityfile": true,
}

// Validate checks the user-visible fields of a server definition. It rejects
// values that could inject additional directives into OpenSSH configuration.
func Validate(s Server) error {
	s.Alias = NormalizeAlias(s.Alias)
	if s.Alias == "" {
		return errf(ErrCodeInvalidAlias, "alias is required")
	}
	if !aliasPattern.MatchString(s.Alias) {
		return errf(ErrCodeInvalidAlias, "alias %q must match %s and contain no whitespace or special characters", s.Alias, aliasPattern.String())
	}
	if reservedAliases[strings.ToLower(s.Alias)] {
		return errf(ErrCodeInvalidAlias, "alias %q is reserved", s.Alias)
	}

	host := strings.TrimSpace(s.Host)
	if host == "" {
		return errf(ErrCodeInvalidHost, "host is required")
	}
	if strings.ContainsAny(host, " \t\r\n=/#") || !hostPattern.MatchString(host) {
		return errf(ErrCodeInvalidHost, "host %q is not a valid hostname or IP", s.Host)
	}

	if s.Port < 0 || s.Port > 65535 {
		return errf(ErrCodeInvalidPort, "port %d out of range", s.Port)
	}
	if s.Port == 0 {
		s.Port = DefaultPort
	}

	user := strings.TrimSpace(s.User)
	if user == "" {
		return errf(ErrCodeInvalidUser, "user is required")
	}
	if !userPattern.MatchString(user) {
		return errf(ErrCodeInvalidUser, "user %q must match %s", s.User, userPattern.String())
	}

	switch s.Auth {
	case AuthKeyPath, AuthKeyInline, AuthPassword:
	default:
		return errf(ErrCodeInvalidAuthMode, "auth mode %q is invalid", s.Auth)
	}

	if s.ProxyJump != "" && !proxyPattern.MatchString(strings.TrimSpace(s.ProxyJump)) {
		return errf(ErrCodeInvalidProxyJump, "proxy jump %q is invalid", s.ProxyJump)
	}

	if len(s.Notes) > 500 {
		return errf(ErrCodeBadPayload, "notes too long (max 500 chars)")
	}
	return nil
}

// ValidateKeyPath ensures a key path is absolute or home-relative and cannot
// smuggle configuration syntax.
func ValidateKeyPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return errf(ErrCodeInvalidKeyPath, "key path is required for key path auth mode")
	}
	if strings.ContainsAny(path, "\r\n") {
		return errf(ErrCodeInvalidKeyPath, "key path must not contain newlines")
	}
	if !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "~/") && !windowsAbsPath(path) {
		return errf(ErrCodeInvalidKeyPath, "key path %q must be absolute or start with ~/", path)
	}
	return nil
}

// NormalizeAlias lowercases an alias; OpenSSH Host matching is
// case-insensitive and lowercasing keeps conflict detection simple.
func NormalizeAlias(alias string) string {
	return strings.ToLower(strings.TrimSpace(alias))
}

const DefaultPort = 22

// windowsAbsPath reports whether path looks like a Windows absolute path
// (drive letter form), so validation also works on Windows hosts.
func windowsAbsPath(path string) bool {
	if len(path) < 3 {
		return false
	}
	c := path[0]
	if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
		return false
	}
	return path[1] == ':' && (path[2] == '\\' || path[2] == '/')
}
