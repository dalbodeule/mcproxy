package buildinfo

// These values are replaced by release builds through -ldflags. Keeping safe
// defaults makes local binaries and tests self-describing as development builds.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)
