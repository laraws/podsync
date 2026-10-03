// Package buildinfo holds metadata populated at build time with linker flags.
package buildinfo

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
	Arch    = ""
)

func DisplayVersion() string {
	if Version == "" {
		return "dev"
	}
	return Version
}
