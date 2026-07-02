package cli

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

func ParseCookieFile(text, host string) (string, error) {
	if host == "" {
		host = "lunars.dev"
	}

	var netscapePairs []string
	var rawLines []string

	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if trimmed == "" {
			continue
		}

		fields := strings.Split(trimmed, "\t")
		if len(fields) >= 7 {
			domain := strings.TrimPrefix(fields[0], "#HttpOnly_")
			if domainMatches(host, domain) {
				netscapePairs = append(netscapePairs, fmt.Sprintf("%s=%s", fields[5], strings.Join(fields[6:], "\t")))
			}
			continue
		}

		if !strings.HasPrefix(trimmed, "#") {
			rawLines = append(rawLines, trimmed)
		}
	}

	if len(netscapePairs) > 0 {
		return strings.Join(dedupeCookiePairs(netscapePairs), "; "), nil
	}
	return NormalizeCookieHeader(strings.Join(rawLines, "\n"))
}

func NormalizeCookieHeader(value string) (string, error) {
	var pairs []string

	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(strings.ToLower(line), "set-cookie:") {
			line = strings.TrimSpace(line[len("set-cookie:"):])
			if pair := strings.TrimSpace(strings.SplitN(line, ";", 2)[0]); strings.Contains(pair, "=") {
				pairs = append(pairs, pair)
			}
			continue
		}

		if strings.HasPrefix(strings.ToLower(line), "cookie:") {
			line = strings.TrimSpace(line[len("cookie:"):])
		}

		for _, part := range strings.Split(line, ";") {
			pair := strings.TrimSpace(part)
			if strings.Contains(pair, "=") {
				pairs = append(pairs, pair)
			}
		}
	}

	if len(pairs) == 0 {
		bareToken := strings.TrimSpace(value)
		if isLikelyBareSessionToken(bareToken) {
			return "__Secure-next-auth.session-token=" + bareToken, nil
		}
		return "", errors.New("cookie value is empty or could not be parsed")
	}

	return strings.Join(dedupeCookiePairs(pairs), "; "), nil
}

func domainMatches(host, domain string) bool {
	normalized := strings.TrimPrefix(strings.ToLower(domain), ".")
	normalizedHost := strings.ToLower(host)
	return normalizedHost == normalized || strings.HasSuffix(normalizedHost, "."+normalized)
}

func isLikelyBareSessionToken(value string) bool {
	if value == "" || strings.Contains(value, "=") {
		return false
	}
	for _, r := range value {
		if unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func dedupeCookiePairs(pairs []string) []string {
	indexByName := make(map[string]int, len(pairs))
	out := make([]string, 0, len(pairs))

	for _, pair := range pairs {
		name := strings.SplitN(pair, "=", 2)[0]
		if index, ok := indexByName[name]; ok {
			out[index] = pair
			continue
		}
		indexByName[name] = len(out)
		out = append(out, pair)
	}

	return out
}
