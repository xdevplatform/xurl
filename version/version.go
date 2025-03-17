package version

// These variables will be set during build time
var (
	// Version is the current version of the application
	Version = "dev"
	// Commit is the git commit SHA at build time
	Commit = "none"
	// BuildDate is the date when the binary was built
	BuildDate = "unknown"
)
