package life

import (
	"regexp"
	"strings"
)

var (
	addrIPv4   = regexp.MustCompile(`[0-9]{1,3}(?:\.[0-9]{1,3}){3}`)
	addrGroups = regexp.MustCompile(`::|(?:[0-9A-Fa-f]{1,4}:){2,}`)
	addrScheme = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://`)
	addrPath   = regexp.MustCompile(`(?:^|[\s(])/[A-Za-z0-9._-]|[A-Za-z]:\\|\\`)
	addrUser   = regexp.MustCompile(`\S@\S`)
	addrDotted = regexp.MustCompile(`[A-Za-z0-9-]\.[A-Za-z0-9-]`)
	addrKey    = regexp.MustCompile(`\b(?:dial|lookup|resolve|connect to)\s+`)
	addrNet    = regexp.MustCompile(`^(?:tcp|udp)\s+`)
)

// causeMaxChars is the bound of a displayed failure cause.
const causeMaxChars = 160

func wordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func labelByte(c byte) bool { return wordByte(c) && c != '_' || c == '-' }

func space(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

// hostPort: a non-space byte, a colon, then one to five digits that are not
// followed by another digit.
func hostPort(s string) bool {
	for i := 1; i < len(s); i++ {
		if s[i] != ':' || space(s[i-1]) {
			continue
		}
		digits := 0
		for i+1+digits < len(s) && s[i+1+digits] >= '0' && s[i+1+digits] <= '9' {
			digits++
		}
		if digits >= 1 && digits <= 5 {
			return true
		}
	}
	return false
}

// operandAt: a host label that is not the word tcp or udp and is followed by a
// colon or the end of the text.
func operandAt(rest string) bool {
	for _, word := range []string{"tcp", "udp"} {
		if strings.HasPrefix(rest, word) && (len(rest) == len(word) || !wordByte(rest[len(word)])) {
			return false
		}
	}
	n := 0
	for n < len(rest) && labelByte(rest[n]) {
		n++
	}
	return n > 0 && (n == len(rest) || rest[n] == ':')
}

func hostOperand(s string) bool {
	for _, key := range addrKey.FindAllStringIndex(s, -1) {
		rest := s[key[1]:]
		if operandAt(rest) {
			return true
		}
		if network := addrNet.FindStringIndex(rest); network != nil && operandAt(rest[network[1]:]) {
			return true
		}
	}
	return false
}

// causeValid reports whether s is an acceptable displayed failure cause: at most
// 160 characters of printable ASCII with no address shape.
func causeValid(s string) bool {
	if len(s) > causeMaxChars {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < ' ' || s[i] > '~' {
			return false
		}
	}
	return !addrIPv4.MatchString(s) && !addrGroups.MatchString(s) && !hostPort(s) && !addrScheme.MatchString(s) &&
		!addrPath.MatchString(s) && !addrUser.MatchString(s) && !addrDotted.MatchString(s) && !hostOperand(s)
}

// Timestamp reports whether v is an RFC 3339 UTC timestamp with the Z
