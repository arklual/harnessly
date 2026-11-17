package outputs

import "testing"

func TestHubDocsPublishIsAnArtifactCommand(t *testing.T) {
	for _, cmd := range []string{"harnessly docs publish plan.md", "cd /w && harnessly doc publish p.html --slug x", "artifacts publish --file a.md"} {
		if !artifactCmdRe.MatchString(cmd) {
			t.Errorf("not recognised: %s", cmd)
		}
	}
	if artifactCmdRe.MatchString("harnessly docs search publish") {
		t.Error("docs search is no publish")
	}
}
