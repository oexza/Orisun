package orisun

// Version is the current version of Orisun.
// This will be overridden during build by the -ldflags="-X 'orisun/orisun.Version=v1.2.3'" flag.
var Version = "dev"

// BuildTime is the time when the binary was built.
// This will be overridden during build by the -ldflags="-X 'orisun/orisun.BuildTime=<timestamp>'" flag.
var BuildTime = "unknown"

// GitCommit is the git commit hash from which the binary was built.
// This will be overridden during build by the -ldflags="-X 'orisun/orisun.GitCommit=<hash>'" flag.
var GitCommit = "unknown"

// GetVersion returns the current version of Orisun.
func GetVersion() string {
	return Version
}

// GetBuildInfo returns the version, build time, and git commit hash.
func GetBuildInfo() (string, string, string) {
	return Version, BuildTime, GitCommit
}
