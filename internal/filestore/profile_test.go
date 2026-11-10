package filestore

import "testing"

// the profile syncs what makes Claude the person's, never a secret.
func TestTheProfileSyncsNoSecret(t *testing.T) {
	p := PolicyFor("profile-sergey")
	if _, ok := p.(Profile); !ok {
		t.Fatalf("a profile room's policy is %T", p)
	}
	for _, rel := range []string{".claude/settings.json", ".claude/CLAUDE.md", ".claude/skills/a/SKILL.md", ".claude/hooks/x.sh",
		".claude/agents/r.md"} {
		if !p.Synced(rel) {
			t.Errorf("%s does not sync", rel)
		}
	}
	for _, rel := range []string{".gitconfig", ".config/glab-cli/config.yml", ".config/acme/github.env",
		".config/acme/runtime.json", ".claude/.credentials.json", ".claude/skills/a/.env", ".claude/hooks/deploy.pem",
		".claude/skills/synced/x/SKILL.md", ".claude/projects/a.jsonl", ".bashrc", ".claude/skills/a/node_modules/x.js",
		".ssh/id_ed25519"} {
		if p.Synced(rel) {
			t.Errorf("%s syncs", rel)
		}
	}
	for _, dir := range []string{".config", ".config/acme", ".ssh", ".claude/projects"} {
		if !p.SkippedDir(dir) {
			t.Errorf("%s is walked", dir)
		}
	}
	if p.SkippedDir(".claude") || p.SkippedDir(".claude/skills") {
		t.Error("the profile's folders are skipped")
	}
	if !KnownKind(KindProfile) {
		t.Error("the profile is no kind the hub serves")
	}
}
