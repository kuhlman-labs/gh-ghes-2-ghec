// Package version provides version information for the application.
package version

// Version is the current version of the application.
// This will be set at build time using ldflags from the Makefile.
var Version = "dev"

// BuildTime is the time the application was built.
// This will be set at build time using ldflags from the Makefile.
var BuildTime = "unknown"

// GetVersion returns the current version string
func GetVersion() string {
	return Version
}

// GetBuildTime returns the build time string
func GetBuildTime() string {
	return BuildTime
}

// GetVersionInfo returns a formatted string with version and build time
func GetVersionInfo() string {
	return "Version: " + Version + " (Built: " + BuildTime + ")"
}
