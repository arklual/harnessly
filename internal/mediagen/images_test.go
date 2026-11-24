package mediagen

import (
	"encoding/json"
	"testing"
)

func TestParseImageCatalogPutsTheFeaturedFirstAndSkipsTools(t *testing.T) {
	var data any
	raw := `{"items":[
		{"id":"soul_2","name":"Higgsfield Soul 2.0","provider_name":"Higgsfield","output_type":"image","medias":[{"max":1}]},
		{"id":"image_background_remover","name":"Image Background Remover","output_type":"image"},
		{"id":"ms_image","name":"DTC Ads","output_type":"image","parameters":[{"name":"style_id","required":"required"}]},
		{"id":"cinematic_studio_image","name":"Cinematic Studio Image","output_type":"image","parameters":[{"name":"camera_model_id","required":"required"}]},
		{"id":"recraft_x","name":"Recraft X","provider_name":"Recraft","output_type":"image"},
		{"id":"nano_banana_pro","name":"Nano Banana Pro","provider_name":"Google","output_type":"image","medias":[{}]},
		{"id":"kling3_0","name":"Kling","output_type":"video"},
		{"id":"Bad ID","name":"x","output_type":"image"},
		{"id":"nameless","name":"","output_type":"image"}
	]}`
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		t.Fatal(err)
	}
	got := parseImageCatalog(data)
	want := []ImageModel{
		{ID: "nano_banana_pro", Name: "Nano Banana Pro", Maker: "Google", Refs: true},
		{ID: "soul_2", Name: "Higgsfield Soul 2.0", Maker: "Higgsfield", Refs: true},
		{ID: "recraft_x", Name: "Recraft X", Maker: "Recraft"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("model %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestImageViaPrefersTheMCPConnection(t *testing.T) {
	s := &Service{}
	if v := s.ImageVia(); v != "" {
		t.Fatalf("nothing connected: %q", v)
	}
	if m := s.ImageModels(t.Context()); m != nil {
		t.Fatalf("models without a provider: %+v", m)
	}
}
