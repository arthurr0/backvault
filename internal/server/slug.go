package server

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func slugify(value string) string {
	var sb strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
			lastDash = false
		case unicode.IsSpace(r), r == '-', r == '_', r == '.', r == '/':
			if !lastDash && sb.Len() > 0 {
				sb.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(sb.String(), "-")
}

func validSlug(value string) bool {
	return slugPattern.MatchString(value)
}

type slugChecker func(ctx context.Context, slug, exceptID string) (bool, error)

func uniqueSlug(ctx context.Context, base, exceptID string, exists slugChecker) (string, error) {
	if base == "" {
		base = "job"
	}
	candidate := base
	for i := 2; i < 1000; i++ {
		taken, err := exists(ctx, candidate, exceptID)
		if err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
		candidate = base + "-" + strconv.Itoa(i)
	}
	return "", newValidationError("could not derive a unique slug").field("slug", "conflict")
}
