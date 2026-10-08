package protocol

import "regexp"

var (
	allowedNameRegex = regexp.MustCompile(`[^a-zA-Z0-9 _-]`)
	minNameLength    = 3
	maxNameLength    = 20
)

// sanitizeName removes all characters not matching [a-zA-Z0-9 _-].
func sanitizeName(name string) string {
	return allowedNameRegex.ReplaceAllString(name, "")
}

// ValidateRename validates and sanitizes a requested nickname during a rename operation.
// If valid (between 3 and 20 characters after sanitization), it returns the sanitized name, empty error, and true.
// If shorter than 3 characters, it returns the currentName, an error message, and false.
func ValidateRename(requestedName string, currentName string) (string, string, bool) {
	sanitized := sanitizeName(requestedName)
	if len(sanitized) < minNameLength {
		return currentName, "Name must be at least 3 characters", false
	}
	if len(sanitized) > maxNameLength {
		sanitized = string([]rune(sanitized)[:maxNameLength])
	}
	return sanitized, "", true
}

// ValidateHelloName validates and sanitizes a requested nickname during the initial hello handshake.
// If shorter than 3 characters (or invalid), it defaults to "Guest".
// If longer than 20 characters, it is truncated to 20 characters.
func ValidateHelloName(requestedName string) string {
	sanitized := sanitizeName(requestedName)
	if len(sanitized) < minNameLength {
		return "Guest"
	}
	if len(sanitized) > maxNameLength {
		sanitized = string([]rune(sanitized)[:maxNameLength])
	}
	return sanitized
}
