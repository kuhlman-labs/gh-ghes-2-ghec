package sanitization

import (
	"strings"
	"testing"
)

func TestSanitizeGenericInput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal input",
			input:    "Hello, World!",
			expected: "Hello, World!",
		},
		{
			name:     "input with dangerous characters",
			input:    "Hello <script>alert('XSS')</script>",
			expected: "Hello  script alert('XSS') /script ",
		},
		{
			name:     "overly long input",
			input:    strings.Repeat("a", MaxInputLength+100),
			expected: strings.Repeat("a", MaxInputLength),
		},
		{
			name:     "input with whitespace",
			input:    "  Hello, World!  ",
			expected: "Hello, World!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeGenericInput(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeGenericInput() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSanitizeHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "text with html tags",
			input:    "<p>Hello, World!</p>",
			expected: "Hello, World!",
		},
		{
			name:     "text with script tag",
			input:    "Hello <script>alert('XSS')</script> World",
			expected: "Hello ('XSS') World",
		},
		{
			name:     "text with javascript: protocol",
			input:    "<a href=\"javascript:alert('XSS')\">Click me</a>",
			expected: "Click me",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeHTML(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeHTML() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSanitizeFilePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple path",
			input:    "path/to/file.txt",
			expected: "path/to/file.txt",
		},
		{
			name:     "path with parent directory traversal",
			input:    "../../../etc/passwd",
			expected: "etc/passwd",
		},
		{
			name:     "path with absolute reference",
			input:    "/etc/passwd",
			expected: "etc/passwd",
		},
		{
			name:     "path with spaces and special characters",
			input:    " path/with spaces/$pecial<chars>.txt ",
			expected: "path/with spaces/$pecial<chars>.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeFilePath(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeFilePath() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSanitizeURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "valid https URL",
			input:    "https://example.com/path",
			expected: "https://example.com/path",
		},
		{
			name:     "URL with non-http scheme",
			input:    "javascript:alert('XSS')",
			expected: "https://",
		},
		{
			name:     "URL with fragment",
			input:    "https://example.com/path#fragment",
			expected: "https://example.com/path",
		},
		{
			name:     "URL with path traversal",
			input:    "https://example.com/../../../etc/passwd",
			expected: "https://example.com/etc/passwd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeURL(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeURL() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSanitizeSQL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal input",
			input:    "John Smith",
			expected: "John Smith",
		},
		{
			name:     "input with single quotes",
			input:    "O'Reilly",
			expected: "O''Reilly",
		},
		{
			name:     "SQL injection attempt",
			input:    "' OR '1'='1",
			expected: "",
		},
		{
			name:     "SQL comment",
			input:    "username -- comment",
			expected: "username  comment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeSQL(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeSQL() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSanitizeJSONKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal key",
			input:    "user_id",
			expected: "user_id",
		},
		{
			name:     "key with quotes",
			input:    "\"user_id\"",
			expected: "user_id",
		},
		{
			name:     "key with escape characters",
			input:    "user_id\n\r\t",
			expected: "user_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeJSONKey(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeJSONKey() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSanitizeHeader(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal header",
			input:    "application/json",
			expected: "application/json",
		},
		{
			name:     "header with newlines",
			input:    "application/json\r\nSet-Cookie: malicious=value",
			expected: "application/jsonSet-Cookie: malicious=value",
		},
		{
			name:     "header with dangerous characters",
			input:    "application/json<script>",
			expected: "application/json script",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeHeader(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeHeader() = %v, want %v", result, tt.expected)
			}
		})
	}
}
