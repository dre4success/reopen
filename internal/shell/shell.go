// Package shell writes command lines for a POSIX shell, which zsh, the
// Mac's own, reads alike.
package shell

import "strings"

// Quote quotes s so a shell reads it as one word, unchanged. A word of only
// safe characters stays bare, so the commands read as the operator would
// type them.
func Quote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("@%+=:,./-_", r)) {
			safe = false
			break
		}
	}
	// zsh, the Mac's shell, reads a word that starts with = as a command's
	// path (=ls is /bin/ls), so such a word is quoted too.
	if safe && s[0] != '=' {
		return s
	}
	// In single quotes nothing is special, not even $, ` or !, so the only
	// case is a quote itself: end the quotes, write an escaped one, reopen.
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Join quotes each argument and joins them with spaces.
func Join(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = Quote(a)
	}
	return strings.Join(q, " ")
}
