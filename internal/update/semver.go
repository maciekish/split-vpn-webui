package update

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	semverPattern     = regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)(?:[-+][A-Za-z0-9._-]+)?$`)
	allowedTagPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

type semverParts struct {
	major int
	minor int
	patch int
}

func normalizeTag(tag string) (string, error) {
	trimmed := strings.TrimSpace(tag)
	if trimmed == "" {
		return "", fmt.Errorf("version tag is required")
	}
	if !allowedTagPattern.MatchString(trimmed) {
		return "", fmt.Errorf("version tag contains unsupported characters")
	}
	return trimmed, nil
}

func parseSemver(tag string) (semverParts, bool) {
	matches := semverPattern.FindStringSubmatch(strings.TrimSpace(tag))
	if len(matches) != 4 {
		return semverParts{}, false
	}
	major, err := strconv.Atoi(matches[1])
	if err != nil {
		return semverParts{}, false
	}
	minor, err := strconv.Atoi(matches[2])
	if err != nil {
		return semverParts{}, false
	}
	patch, err := strconv.Atoi(matches[3])
	if err != nil {
		return semverParts{}, false
	}
	return semverParts{major: major, minor: minor, patch: patch}, true
}

func isNewerVersion(current, candidate string) bool {
	currentTag := strings.TrimSpace(current)
	candidateTag := strings.TrimSpace(candidate)
	if candidateTag == "" {
		return false
	}
	if currentTag == "" {
		return true
	}
	currentSemver, currentOK := parseSemver(currentTag)
	candidateSemver, candidateOK := parseSemver(candidateTag)
	if currentOK && candidateOK {
		if candidateSemver.major != currentSemver.major {
			return candidateSemver.major > currentSemver.major
		}
		if candidateSemver.minor != currentSemver.minor {
			return candidateSemver.minor > currentSemver.minor
		}
		return candidateSemver.patch > currentSemver.patch
	}
	return candidateTag != currentTag
}
