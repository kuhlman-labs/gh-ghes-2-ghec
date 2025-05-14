// Package sanitization provides utilities for cleaning and sanitizing user inputs
// to prevent security vulnerabilities like XSS, path traversal, and SQL injection.
package sanitization

import (
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// Common sanitization constants
const (
	// MaxInputLength defines the maximum length for generic string inputs
	MaxInputLength = 1024
)

var (
	// Pattern for allowed characters in generic string inputs
	allowedCharsPattern = regexp.MustCompile(`^[a-zA-Z0-9\s\-_.,;:!?@#$%^&*()[\]{}|<>/+=]*$`)

	// Pattern for HTML/script tags that should be removed
	htmlTagsPattern = regexp.MustCompile(`<[^>]*>`)

	// Pattern for common script injection vectors
	scriptPattern = regexp.MustCompile(`(?i)(javascript:|data:text/html|vbscript:|livescript:)`)

	// Pattern for SQL injection attempts
	sqlInjectionPattern = regexp.MustCompile(`(?i)('\s*or\s*'|'\s*and\s*'|'\s*union\s*'|exec\s*\(|eval\s*\()`)
)

// SanitizeGenericInput cleans a generic string input by:
// - Trimming whitespace
// - Limiting its length
// - Removing potentially dangerous characters
func SanitizeGenericInput(input string) string {
	// Trim whitespace
	input = strings.TrimSpace(input)

	// Limit length
	if len(input) > MaxInputLength {
		input = input[:MaxInputLength]
	}

	// If input doesn't match allowed pattern, clean it
	if !allowedCharsPattern.MatchString(input) {
		// Replace disallowed characters with spaces
		replacer := strings.NewReplacer(
			"<", " ",
			">", " ",
			"`", " ",
			"$", " ",
			"\\", " ",
		)
		input = replacer.Replace(input)
	}

	return input
}

// SanitizeHTML removes HTML tags and script content from a string
func SanitizeHTML(input string) string {
	// Remove HTML tags
	input = htmlTagsPattern.ReplaceAllString(input, " ")

	// Remove script protocol handlers
	input = scriptPattern.ReplaceAllString(input, "")

	// Remove common script keywords and XSS vectors
	dangerousWords := []string{
		"script", "alert", "eval", "javascript", "onerror", "onload", "onclick",
		"onfocus", "onmouseover", "onmouseout", "onsubmit", "prompt", "confirm",
		"document.cookie", "window.location", "document.write", "innerHTML",
	}

	inputLower := strings.ToLower(input)
	for _, word := range dangerousWords {
		if strings.Contains(inputLower, word) {
			// Replace the word with spaces, maintaining the original case
			pattern := regexp.MustCompile(`(?i)` + word)
			input = pattern.ReplaceAllStringFunc(input, func(matched string) string {
				return strings.Repeat(" ", len(matched))
			})
		}
	}

	// Clean up multiple spaces
	for strings.Contains(input, "  ") {
		input = strings.ReplaceAll(input, "  ", " ")
	}

	return strings.TrimSpace(input)
}

// SanitizeFilePath sanitizes a file path to prevent path traversal attacks
func SanitizeFilePath(input string) string {
	// First clean the input of any problematic characters
	input = SanitizeGenericInput(input)

	// Clean path to resolve .. and other potentially dangerous elements
	input = filepath.Clean(input)

	// Remove leading slashes or drive indicators to make it relative
	input = strings.TrimLeft(input, "/\\.")

	// Remove any directory traversal sequences
	parts := strings.Split(input, "/")
	var cleanParts []string

	for _, part := range parts {
		// Skip parent directory references and empty parts
		if part != ".." && part != "." && part != "" {
			cleanParts = append(cleanParts, part)
		}
	}

	// Reconstruct the clean path
	if len(cleanParts) == 0 {
		return ""
	}

	return strings.Join(cleanParts, "/")
}

// SanitizeURL sanitizes a URL by ensuring it's properly formatted and
// removing potentially dangerous components
func SanitizeURL(input string) string {
	// Handle empty input
	if input == "" {
		return ""
	}

	// Add https:// if no scheme is present to help url.Parse
	if !strings.Contains(input, "://") {
		if !strings.HasPrefix(input, "http://") && !strings.HasPrefix(input, "https://") {
			input = "https://" + input
		}
	}

	// Parse the URL
	parsedURL, err := url.Parse(input)
	if err != nil {
		// If parsing fails, return empty string
		return "https://"
	}

	// Ensure scheme is http or https
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		parsedURL.Scheme = "https"
	}

	// Validate host is present
	if parsedURL.Host == "" {
		return "https://"
	}

	// Clean host
	parsedURL.Host = strings.TrimSpace(parsedURL.Host)

	// Clean path
	parsedURL.Path = filepath.Clean(parsedURL.Path)

	// Remove fragments
	parsedURL.Fragment = ""

	// Return the sanitized URL
	return parsedURL.String()
}

// SanitizeSQL sanitizes a string to prevent SQL injection
// Note: This is a basic implementation. For real SQL safety, use
// parameterized queries and prepared statements instead.
func SanitizeSQL(input string) string {
	// Check for common SQL injection patterns
	if sqlInjectionPattern.MatchString(input) {
		// If suspicious patterns are found, return empty string
		return ""
	}

	// Escape single quotes
	input = strings.ReplaceAll(input, "'", "''")

	// Remove other potentially dangerous characters
	input = strings.ReplaceAll(input, ";", "")
	input = strings.ReplaceAll(input, "--", "")
	input = strings.ReplaceAll(input, "/*", "")
	input = strings.ReplaceAll(input, "*/", "")

	return input
}

// SanitizeJSONKey sanitizes a string for use as a JSON key
func SanitizeJSONKey(input string) string {
	// Remove potentially dangerous characters
	replacer := strings.NewReplacer(
		"\"", "",
		"\\", "",
		"\b", "",
		"\f", "",
		"\n", "",
		"\r", "",
		"\t", "",
	)

	return replacer.Replace(input)
}

// SanitizeHeader sanitizes an HTTP header value
func SanitizeHeader(input string) string {
	// Replace newlines and carriage returns which could be used for header injection
	input = strings.ReplaceAll(input, "\n", "")
	input = strings.ReplaceAll(input, "\r", "")

	// Apply generic input sanitization to remove dangerous characters
	input = SanitizeGenericInput(input)

	// Replace dangerous characters that might be used for XSS in headers
	replacer := strings.NewReplacer(
		"<", " ",
		">", " ",
		"script", " script ",
		"alert", " alert ",
	)

	input = replacer.Replace(input)

	// Clean up multiple spaces
	for strings.Contains(input, "  ") {
		input = strings.ReplaceAll(input, "  ", " ")
	}

	return strings.TrimSpace(input)
}
