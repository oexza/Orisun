package common

// Version is the current version of Orisun.
// This will be overridden during build by the -ldflags="-X 'github.com/oexza/orisun/common.Version=v1.2.3'" flag.
var Version = "dev"

// GetVersion returns the current version of Orisun.
func GetVersion() string {
	return Version
}