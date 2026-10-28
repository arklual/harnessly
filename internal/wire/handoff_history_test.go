package wire

import "testing"

const testArtifactID = "0123456789abcdef0123456789abcdef"

func TestHandoffHistoryValidationIsModeSpecific(t *testing.T) {
	base := ConversationHandoffCmd{OpID: "h1", ID: "tab-source_123", Provider: ProviderCodex, Model: nil}
	tests := []struct {
		name string
		cmd  ConversationHandoffCmd
		ok   bool
	}{
		{"history artifact", withHandoff(base, HandoffHistory, "", testArtifactID), true},
		{"history without artifact", withHandoff(base, HandoffHistory, "", ""), false},
		{"history with text", withHandoff(base, HandoffHistory, "context", testArtifactID), false},
		{"history traversal", withHandoff(base, HandoffHistory, "", "../history"), false},
		{"excerpt text", withHandoff(base, HandoffExcerpt, "context", ""), true},
		{"excerpt without text", withHandoff(base, HandoffExcerpt, "", ""), false},
		{"excerpt with artifact", withHandoff(base, HandoffExcerpt, "context", testArtifactID), false},
		{"note text", withHandoff(base, HandoffNote, "context", ""), true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.cmd.Validate() == nil; got != test.ok {
				t.Fatalf("Validate success = %v, want %v: %v", got, test.ok, test.cmd.Validate())
			}
		})
	}
}

func withHandoff(base ConversationHandoffCmd, mode HandoffMode, text, artifactID string) ConversationHandoffCmd {
	base.Mode, base.Text, base.ArtifactID = mode, text, artifactID
	return base
}

func TestHandoffDraftHistoryCarriesOnlyArtifactMetadata(t *testing.T) {
	tests := []struct {
		name  string
		draft HandoffDraft
		ok    bool
	}{
		{"history", HandoffDraft{Mode: DraftHistory, Artifact: &HandoffArtifact{ID: testArtifactID, Bytes: 42}}, true},
		{"history without artifact", HandoffDraft{Mode: DraftHistory}, false},
		{"history with text", HandoffDraft{Mode: DraftHistory, Text: "context", Artifact: &HandoffArtifact{ID: testArtifactID, Bytes: 42}}, false},
		{"excerpt", HandoffDraft{Mode: DraftExcerpt, Text: "context", Tokens: 2, Turns: 1}, true},
		{"excerpt with artifact", HandoffDraft{Mode: DraftExcerpt, Text: "context", Artifact: &HandoffArtifact{ID: testArtifactID, Bytes: 42}}, false},
		{"invalid artifact", HandoffDraft{Mode: DraftHistory, Artifact: &HandoffArtifact{ID: "../x", Bytes: 0}}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.draft.Validate() == nil; got != test.ok {
				t.Fatalf("Validate success = %v, want %v: %v", got, test.ok, test.draft.Validate())
			}
		})
	}
}
