package formula

import (
	"regexp"
	"testing"

	"github.com/steveyegge/gastown/internal/channelevents"
)

// channelFlagRe matches `--channel <value>` arguments inside formula shell snippets
// (used by both `gt mol step await-event` and `gt mol step emit-event`).
var channelFlagRe = regexp.MustCompile(`--channel[= ]"?([a-zA-Z0-9_{}.-]+)"?`)

// TestFormulaChannelsMatchEmitters guards against formula channel names drifting
// from the Go channel-naming helpers. gt-v5d4 moved refinery events from a shared
// "refinery" channel to a per-rig "refinery-<rig>" channel on the Go side, but the
// channel name also lives as a bare string literal in mol-refinery-patrol's TOML,
// which no compiler or Go test covers — that drift shipped silently and armed
// gt-kq1b. Every --channel argument that looks like a refinery channel must match
// channelevents.RefineryChannel's naming scheme.
func TestFormulaChannelsMatchEmitters(t *testing.T) {
	embedded, err := getEmbeddedFormulas()
	if err != nil {
		t.Fatalf("getEmbeddedFormulas() error: %v", err)
	}

	wantRefineryChannel := channelevents.RefineryChannel("{{rig}}")

	for name := range embedded {
		content, err := formulasFS.ReadFile("formulas/" + name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}

		for _, m := range channelFlagRe.FindAllStringSubmatch(string(content), -1) {
			channel := m[1]
			if channel == "refinery" {
				t.Errorf("%s: --channel %q is unscoped; refinery channels must be per-rig (%q), see channelevents.RefineryChannel (gt-v5d4/gt-kq1b)",
					name, channel, wantRefineryChannel)
				continue
			}
			if regexp.MustCompile(`^refinery-`).MatchString(channel) && channel != wantRefineryChannel {
				t.Errorf("%s: --channel %q does not match the refinery channel template %q",
					name, channel, wantRefineryChannel)
			}
		}
	}
}
