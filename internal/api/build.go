package api

import (
	"runtime"
	"runtime/debug"

	"github.com/ordo-systems/keen-proxy/internal/engine"
)

// Release builds link the tag and commit from the same checked-out source.
var releaseVersion, releaseRevision string

// buildIdentity contains only public metadata about the running executable.
type buildIdentity struct {
	Version  string `json:"version,omitempty"`
	Variant  string `json:"variant"`
	Arch     string `json:"arch"`
	Revision string `json:"revision,omitempty"`
	Dirty    bool   `json:"dirty,omitempty"`
}

var runningBuild = func() buildIdentity {
	info, _ := debug.ReadBuildInfo()
	return readRunningBuild(info)
}()

func readRunningBuild(info *debug.BuildInfo) buildIdentity {
	build := buildIdentity{Version: releaseVersion, Variant: engine.BuildVariant, Arch: runtime.GOARCH, Revision: releaseRevision}
	if info != nil && build.Revision == "" {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				build.Revision = setting.Value
			case "vcs.modified":
				build.Dirty = setting.Value == "true"
			}
		}
	}
	build.Revision = build.Revision[:min(7, len(build.Revision))]
	return build
}
