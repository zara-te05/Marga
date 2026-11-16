// Package version guarda la versión del binario. GoReleaser la rellena al compilar:
//
//	-ldflags "-X github.com/zara-te05/Marga/internal/version.Version=v1.2.3"
package version

import "runtime/debug"

var (
	Version = "dev"
	Commit  = ""
)

// Texto devuelve algo como "v1.2.3 (a1b2c3d)" o "dev (a1b2c3d)".
func Texto() string {
	c := Commit
	if c == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, s := range info.Settings {
				if s.Key == "vcs.revision" && len(s.Value) >= 7 {
					c = s.Value[:7]
				}
			}
		}
	}
	if c == "" {
		return Version
	}
	return Version + " (" + c + ")"
}
