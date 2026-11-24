package mediagen

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/extproviders"
)

// ImageModel is a picture model the person may pick in a generator list: what the panel shows and what a request
// names as its model.
type ImageModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Maker is the company of the model (Google, OpenAI, …), "" when the catalog does not say.
	Maker string `json:"maker,omitempty"`
	// Refs: the model takes a reference picture, so it can change a picture, not only draw a new one.
	Refs bool `json:"refs,omitempty"`
}

// featuredImages come first in the list, in this order: the strongest general models of the catalog. The rest follow
// as the catalog lists them.
var featuredImages = []string{"gpt_image_2_6", "nano_banana_pro", "nano_banana_2_1", "seedream_v5_pro", "flux_3_image",
	"soul_2", "ideogram_4_6", "kling_image_4_0", "grok_image_2_0", "recraft_v4_1", "z_image"}

// skippedImages are tools of the catalog, not generators of a picture from words (upscalers, background removers,
// outpainting) or duplicates and studio presets that need settings the panel has no place for.
var skippedImages = map[string]bool{
	"image_background_remover": true, "outpaint": true, "flux_2_pro_outpaint": true, "image_decompose": true,
	"bytedance_image_upscale": true, "topaz_image": true, "topaz_image_generative": true, "image_command": true,
	"3d_jutsu_angles": true, "nano_banana_2_shots": true, "soul_v2": true, "soul_cinema_studio": true, "ms_image": true,
	"soul_cast": true, "soul_location": true, "cinematic_studio_soul_cast": true, "cinematic_studio_soul_location": true,
	"ideogram_4_6_precise_edit": true, "image_auto": true,
}

// fallbackImages is the list when the catalog cannot be read: the featured models, by what they are known as.
var fallbackImages = []ImageModel{
	{ID: "gpt_image_2_6", Name: "GPT Image 2.6", Maker: "OpenAI", Refs: true},
	{ID: "nano_banana_pro", Name: "Nano Banana Pro", Maker: "Google", Refs: true},
	{ID: "nano_banana_2_1", Name: "Nano Banana 2.1", Maker: "Google", Refs: true},
	{ID: "seedream_v5_pro", Name: "Seedream 5.0 Pro", Maker: "Bytedance", Refs: true},
	{ID: "flux_3_image", Name: "FLUX 3 Image", Maker: "Black Forest Labs", Refs: true},
	{ID: "soul_2", Name: "Higgsfield Soul 2.0", Maker: "Higgsfield", Refs: true},
	{ID: "z_image", Name: "Z Image", Maker: "Tongyi-MAI"},
}

// imageCatalogTTL is how long the catalog read from the MCP is kept.
const imageCatalogTTL = 30 * time.Minute

// ImageVia says how a picture asked of Higgsfield without naming an account is made: "mcp" (the person's MCP
// connection: their plan's credits), "key" (a REST API key), or "" when neither is there. The MCP comes first: the
// subscription's credits are spendable only through it.
func (s *Service) ImageVia() string {
	if s.MCP != nil && s.MCP.Connected() {
		return "mcp"
	}
	if _, err := s.resolveKey(""); err == nil {
		return "key"
	}
	return ""
}

// ImageModels is the list of picture models of the way ImageVia names: the MCP's catalog (read live, kept for a while,
// the known list when it cannot be read), or the REST API's few models (they draw from words only).
func (s *Service) ImageModels(ctx context.Context) []ImageModel {
	switch s.ImageVia() {
	case "mcp":
		s.mu.Lock()
		cached, at := s.imageCatalog, s.imageCatalogAt
		s.mu.Unlock()
		if len(cached) > 0 && s.now().Sub(at) < imageCatalogTTL {
			return slices.Clone(cached)
		}
		list, err := s.readImageCatalog(ctx)
		if err != nil || len(list) == 0 {
			if len(cached) > 0 {
				return slices.Clone(cached)
			}
			return slices.Clone(fallbackImages)
		}
		s.mu.Lock()
		s.imageCatalog, s.imageCatalogAt = list, s.now()
		s.mu.Unlock()
		return slices.Clone(list)
	case "key":
		var out []ImageModel
		for _, m := range extproviders.HiggsfieldCatalog {
			if m.Kind == extproviders.KindImage {
				out = append(out, ImageModel{ID: m.Family, Name: strings.TrimSuffix(m.Label, " · picture")})
			}
		}
		return out
	}
	return nil
}

func (s *Service) readImageCatalog(ctx context.Context) ([]ImageModel, error) {
	m := &hfMCP{call: s.MCP.Call, http: s.client(), allowHTTP: s.AllowHTTP}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	data, text, err := m.do(ctx, "models_explore", map[string]any{"action": "list", "type": "image", "limit": 100}, "the list of models")
	if err != nil {
		return nil, err
	}
	if data == nil {
		// The list is JSON followed by words about the trial: read the first value only.
		_ = json.NewDecoder(strings.NewReader(strings.TrimSpace(text))).Decode(&data)
	}
	return parseImageCatalog(data), nil
}

// parseImageCatalog reads models_explore's {items: [{id, name, provider_name, medias, parameters}]}: the models a
// panel can run from words alone, the featured first.
func parseImageCatalog(data any) []ImageModel {
	root, _ := data.(map[string]any)
	items, _ := root["items"].([]any)
	byID := map[string]ImageModel{}
	var order []string
	for _, it := range items {
		o, _ := it.(map[string]any)
		id, _ := o["id"].(string)
		name, _ := o["name"].(string)
		if id == "" || strings.TrimSpace(name) == "" || skippedImages[id] || !mcpModelID.MatchString(id) {
			continue
		}
		if out, _ := o["output_type"].(string); out != "" && out != "image" {
			continue
		}
		if needsSetting(o) {
			continue
		}
		maker, _ := o["provider_name"].(string)
		medias, _ := o["medias"].([]any)
		if _, seen := byID[id]; !seen {
			order = append(order, id)
		}
		byID[id] = ImageModel{ID: id, Name: strings.TrimSpace(name), Maker: strings.TrimSpace(maker), Refs: len(medias) > 0}
	}
	var out []ImageModel
	for _, id := range featuredImages {
		if m, ok := byID[id]; ok {
			out = append(out, m)
			delete(byID, id)
		}
	}
	for _, id := range order {
		if m, ok := byID[id]; ok {
			out = append(out, m)
		}
	}
	return out
}

// needsSetting: a parameter the model requires (a style, a camera) that a panel cannot fill.
func needsSetting(model map[string]any) bool {
	params, _ := model["parameters"].([]any)
	for _, p := range params {
		o, _ := p.(map[string]any)
		if req, _ := o["required"].(string); req == "required" {
			return true
		}
	}
	return false
}

// ImageModelRefs says whether a model of ImageModels takes a reference picture (it may change one); an unknown model
// is taken to.
func (s *Service) ImageModelRefs(ctx context.Context, id string) bool {
	for _, m := range s.ImageModels(ctx) {
		if m.ID == id {
			return m.Refs
		}
	}
	return true
}
