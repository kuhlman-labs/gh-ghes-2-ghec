package validation

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// General validation constants
const (
	MaxRepositories         = 100     // Maximum number of repositories per request
	MaxOrgNameLength        = 39      // GitHub org name length limit
	MaxRepoNameLength       = 100     // GitHub repo name length limit
	MaxDurationLimit        = 168     // Maximum migration duration in hours (1 week)
	MinTokenLength          = 30      // Minimum GitHub token length
	DefaultMaxDuration      = 24      // Default max duration in hours
	MaxRequestBodySizeBytes = 1 << 20 // 1MB
)

// Regex patterns
var (
	// GitHub org name pattern (alphanumeric and hyphens, can't start with hyphen)
	OrgNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][\w.-]*$`)

	// GitHub repo name pattern (alphanumeric, periods, hyphens, underscores)
	RepoNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][\w.-]*$`)

	// GitHub token format (40 characters, hexadecimal for classic PATs)
	// Also supports GitHub App token format
	TokenPattern = regexp.MustCompile(`^(gh[ps]_[A-Za-z0-9_]{36}|[a-f0-9]{40})$`)
)

// ValidateURL validates a URL string
func ValidateURL(urlStr string) error {
	if urlStr == "" {
		return fmt.Errorf("URL is required")
	}

	// Parse the URL
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	// Check scheme
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("URL scheme must be http or https")
	}

	// Check for hostname
	if parsedURL.Host == "" {
		return fmt.Errorf("URL must contain a valid hostname")
	}

	return nil
}

// ValidateGitHubToken validates a GitHub token
func ValidateGitHubToken(token string) error {
	if token == "" {
		return fmt.Errorf("token is required")
	}

	if len(token) < MinTokenLength {
		return fmt.Errorf("token is too short, must be at least %d characters", MinTokenLength)
	}

	// Optional: strictly validate token format - uncomment if needed
	// if !TokenPattern.MatchString(token) {
	//    return fmt.Errorf("token has invalid format")
	// }

	return nil
}

// ValidateOrganizationName validates a GitHub organization name
func ValidateOrganizationName(org string) error {
	if org == "" {
		return fmt.Errorf("organization name is required")
	}

	if len(org) > MaxOrgNameLength {
		return fmt.Errorf("organization name exceeds maximum length of %d characters", MaxOrgNameLength)
	}

	if !OrgNamePattern.MatchString(org) {
		return fmt.Errorf("organization name contains invalid characters")
	}

	return nil
}

// ValidateRepositoryName validates a GitHub repository name
func ValidateRepositoryName(repo string) error {
	if repo == "" {
		return fmt.Errorf("repository name is required")
	}

	if len(repo) > MaxRepoNameLength {
		return fmt.Errorf("repository name exceeds maximum length of %d characters", MaxRepoNameLength)
	}

	if !RepoNamePattern.MatchString(repo) {
		return fmt.Errorf("repository name contains invalid characters")
	}

	return nil
}

// ValidateRepositoryList validates a list of repository names
func ValidateRepositoryList(repos []string) error {
	if len(repos) == 0 {
		return fmt.Errorf("repository list cannot be empty")
	}

	if len(repos) > MaxRepositories {
		return fmt.Errorf("too many repositories, maximum allowed is %d", MaxRepositories)
	}

	// Check for duplicates and validate each repository name
	seen := make(map[string]bool)
	for _, repo := range repos {
		if err := ValidateRepositoryName(repo); err != nil {
			return err
		}

		// Check for duplicates (case insensitive)
		repoLower := strings.ToLower(repo)
		if seen[repoLower] {
			return fmt.Errorf("duplicate repository name: %s", repo)
		}
		seen[repoLower] = true
	}

	return nil
}

// ValidateDuration validates a duration string
func ValidateDuration(durationStr string) (time.Duration, error) {
	if durationStr == "" {
		// Return default duration
		return time.Duration(DefaultMaxDuration) * time.Hour, nil
	}

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return 0, fmt.Errorf("invalid duration format: %w", err)
	}

	// Check if duration exceeds maximum
	if duration > time.Duration(MaxDurationLimit)*time.Hour {
		return 0, fmt.Errorf("duration exceeds maximum allowed of %d hours", MaxDurationLimit)
	}

	// Check if duration is negative
	if duration <= 0 {
		return 0, fmt.Errorf("duration must be positive")
	}

	return duration, nil
}

// TestGHESURL attempts to connect to the GHES URL to validate it
func TestGHESURL(baseURL string, token string) error {
	if err := ValidateURL(baseURL); err != nil {
		return err
	}

	// Ensure URL ends with no trailing slash
	baseURL = strings.TrimSuffix(baseURL, "/")

	// Test connectivity to GHES API
	apiURL := baseURL + "/api/v3/meta"
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to GHES instance: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to authenticate with GHES instance: status code %d", resp.StatusCode)
	}

	return nil
}
