package feed

import (
	"os"
	"slices"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

func toolByName(t *testing.T, f *Feed, name string) wire.Entry {
	t.Helper()
	for _, e := range f.b.entries {
		if e.T == wire.EntryTool && e.Tool == name {
			return e
		}
	}
	t.Fatalf("no %s step\n%s", name, shape(f))
	return wire.Entry{}
}

// Codex writes no start event: the script's step says how many pictures to expect, each finished picture joins it by its
// saved path, and the end of the script settles the card.
func TestCodexImageScriptCarriesItsPictures(t *testing.T) {
	raw, err := os.ReadFile("testdata/codex_image_gen.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	f := New("c1", wire.ProviderCodex, &CodexSource{Path: write(t, string(raw))})
	f.Load()
	validate(t, f)
	img := toolByName(t, f, "mcp__image_gen__imagegen").Images
	if img == nil {
		t.Fatal("no picture card")
	}
	if img.Count != 2 || img.Prompt != "A red circle on white" || img.State != "done" || len(img.Done) != 1 ||
		img.Done[0] != "/home/u/.codex/generated_images/t1/exec-1.png" || img.Failed == nil || *img.Failed != "content policy" {
		t.Fatalf("card: %+v", img)
	}
}

func TestHarnesslyPicturesCardFollowsItsJob(t *testing.T) {
	call := `{"type":"assistant","uuid":"00000000-0000-4000-8000-000000000001","timestamp":"2026-10-31T10:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_g","name":"mcp__harnessly__generate_images","input":{"prompt":"a fox","count":3,"shape":"landscape"}}]}}` + "\n"
	still := `{"type":"user","timestamp":"2026-10-31T10:00:50.000Z","message":{"role":"user","content":[{"tool_use_id":"toolu_g","type":"tool_result","content":[{"type":"text","text":"job_id: j1\nStill drawing: 1 of 3 ready. Call wait_images with this job_id to wait for the rest.\n1. /d/images/j1-1.png"}],"is_error":false}]}}` + "\n"
	wait := `{"type":"assistant","uuid":"00000000-0000-4000-8000-000000000002","timestamp":"2026-10-31T10:00:51.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_w","name":"mcp__harnessly__wait_images","input":{"job_id":"j1"}}]}}` + "\n"
	done := `{"type":"user","timestamp":"2026-10-31T10:01:10.000Z","message":{"role":"user","content":[{"tool_use_id":"toolu_w","type":"tool_result","content":"job_id: j1\nDone: 3 picture(s).\n1. /d/images/j1-1.png\n2. /d/images/j1-2.png\n3. /d/images/j1-3.png\nThe person already sees these pictures in the chat; do not paste the paths back unless asked.","is_error":false}]}}` + "\n"
	text := `{"type":"user","timestamp":"2026-10-31T10:00:00.000Z","message":{"role":"user","content":"draw"}}` + "\n" + call

	path := write(t, text)
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
	f.Load()
	img := toolByName(t, f, "mcp__harnessly__generate_images").Images
	if img == nil || img.State != "drawing" || img.Count != 3 || img.Shape != "landscape" || img.Prompt != "a fox" || len(img.Done) != 0 {
		t.Fatalf("pending card: %+v", img)
	}

	f = New("c1", wire.ProviderClaude, &ClaudeSource{Path: write(t, text+still)})
	f.Load()
	if img = toolByName(t, f, "mcp__harnessly__generate_images").Images; img.State != "drawing" || img.JobID != "j1" || len(img.Done) != 1 {
		t.Fatalf("still drawing: %+v", img)
	}

	f = New("c1", wire.ProviderClaude, &ClaudeSource{Path: write(t, text+still+wait+done)})
	f.Load()
	validate(t, f)
	img = toolByName(t, f, "mcp__harnessly__generate_images").Images
	if img.State != "done" || len(img.Done) != 3 || img.Done[2] != "/d/images/j1-3.png" {
		t.Fatalf("after wait_images: %+v", img)
	}
	if toolByName(t, f, "mcp__harnessly__wait_images").Images != nil {
		t.Fatal("the wait step is not a card of its own")
	}
	// the turn lists what it drew, so the folded turn shows the pictures under its answer.
	if got := f.b.turns[len(f.b.turns)-1].rec.Pictures; !slices.Equal(got, []string{"/d/images/j1-1.png", "/d/images/j1-2.png", "/d/images/j1-3.png"}) {
		t.Fatalf("turn pictures: %v", got)
	}
}

func TestAFailedPictureCallIsAFailedCard(t *testing.T) {
	text := `{"type":"user","timestamp":"2026-10-31T10:00:00.000Z","message":{"role":"user","content":"draw"}}` + "\n" +
		`{"type":"assistant","uuid":"00000000-0000-4000-8000-000000000001","timestamp":"2026-10-31T10:00:01.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_g","name":"mcp__harnessly__generate_images","input":{"prompt":"a fox"}}]}}` + "\n" +
		`{"type":"user","timestamp":"2026-10-31T10:00:50.000Z","message":{"role":"user","content":[{"tool_use_id":"toolu_g","type":"tool_result","content":"job_id: j2\nFailed: model refused","is_error":true}]}}` + "\n"
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: write(t, text)})
	f.Load()
	validate(t, f)
	img := toolByName(t, f, "mcp__harnessly__generate_images").Images
	if img.State != "failed" || img.Failed == nil || *img.Failed != "model refused" || img.Count != 1 {
		t.Fatalf("card: %+v", img)
	}
}

// harnessly's generate_images from Codex makes the card whichever way it is called — as a tool of its own
// (function_call) or from a code-mode script — and its answer fills the card with the files.
func TestCodexHarnesslyPicturesMakeTheCard(t *testing.T) {
	user := `{"timestamp":"2026-10-31T13:56:40.000Z","type":"event_msg","payload":{"type":"user_message","message":"нарисуй кота","images":[],"local_images":[]}}` + "\n"
	report := `job_id: j9\nDone: 2 picture(s).\n1. /d/images/j9-1.png\n2. /d/images/j9-2.png\nThe person already sees these pictures in the chat; do not paste the paths back unless asked.`
	cases := []struct {
		name, tool, lines string
	}{
		{"direct", "mcp__harnessly__generate_images",
			`{"timestamp":"2026-10-31T13:56:50.000Z","type":"response_item","payload":{"type":"function_call","name":"mcp__harnessly__generate_images","arguments":"{\"prompt\":\"a cat\",\"count\":2,\"shape\":\"square\"}","call_id":"call_g"}}` + "\n" +
				`{"timestamp":"2026-10-31T13:57:30.000Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call_g","output":"` + report + `"}}` + "\n"},
		{"script", "mcp__harnessly__generate_images",
			`{"timestamp":"2026-10-31T13:56:50.000Z","type":"response_item","payload":{"type":"custom_tool_call","id":"ctc_1","status":"completed","call_id":"call_s","name":"exec","input":"text(await tools.harnessly__generate_images({prompt:\"a cat\",count:2}));"}}` + "\n" +
				`{"timestamp":"2026-10-31T13:57:30.000Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call_s","output":[{"type":"input_text","text":"Script completed\nWall time 40.1 seconds\nOutput:\n` + report + `"}]}}` + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := New("c1", wire.ProviderCodex, &CodexSource{Path: write(t, user+tc.lines)})
			f.Load()
			validate(t, f)
			img := toolByName(t, f, tc.tool).Images
			if img == nil || img.State != "done" || img.Count != 2 || img.Prompt != "a cat" || len(img.Done) != 2 || img.Done[1] != "/d/images/j9-2.png" {
				t.Fatalf("card: %+v", img)
			}
		})
	}
}
