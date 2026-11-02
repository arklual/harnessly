package providers

import "slices"

// The keys of the speech recognition providers live in the same store as the keys of the backend profiles (one file
// each, <Dir>/<id>.key); they are no profiles: no agent runs on them, so Presets does not list them.
const (
	SpeechOpenAI = "openai"
	// SpeechYandex is a Yandex Cloud API key of a service account; SpeechYandexFolder the folder id some accounts
	// send beside it (it is no secret, but is kept and shown the same way).
	SpeechYandex       = "yandex"
	SpeechYandexFolder = "yandex-folder"
)

// SpeechKeys are the ids of the speech providers' keys.
var SpeechKeys = []string{SpeechOpenAI, SpeechYandex, SpeechYandexFolder}

// IsSpeechKey says id names the key of a speech provider.
func IsSpeechKey(id string) bool { return slices.Contains(SpeechKeys, id) }
