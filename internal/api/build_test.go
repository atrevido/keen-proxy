package api

import (
	"runtime/debug"
	"testing"
)

func TestLocalBuildIdentity(t *testing.T) {
	for _, modified := range []string{"true", "false"} {
		t.Run(modified, func(t *testing.T) {
			got := readRunningBuild(&debug.BuildInfo{Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "123456789abcdef"},
				{Key: "vcs.modified", Value: modified},
			}})
			if got.Revision != "1234567" || got.Dirty != (modified == "true") {
				t.Fatalf("unexpected local build metadata: %+v", got)
			}
		})
	}
	if got := readRunningBuild(nil); got.Revision != "" || got.Dirty {
		t.Fatalf("expected no source metadata without VCS info: %+v", got)
	}
}
