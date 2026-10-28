// Package engine is the speech recognition the runtime brings with it: a pinned whisper.cpp server run as a child of
// the runtime, and a Unix socket speaking the protocol internal/voice already knows, so that a machine with nothing
// set up dictates out of the box. The model is fetched once, on the first dictation, checked against its SHA-256 and
// kept in the runtime's data folder; nothing is sent anywhere but the download requests, and neither audio nor text
// is written to disk or logged.
package engine

// WhisperVersion is the whisper.cpp release the server binary is built from (MIT, github.com/ggml-org/whisper.cpp).
const WhisperVersion = "v1.9.5"

// Model is one multilingual ggml model (MIT, Whisper by OpenAI, converted by the whisper.cpp project).
type Model struct {
	ID     string
	File   string
	Size   int64
	SHA256 string
}

// Models are the sizes the engine knows, quantized to 5 bits: "tiny" is the fastest and the least exact, "base" is
// what a 2-vCPU machine answers a 30-second recording with in a few seconds, "small" is clearly better in Russian and
// takes about three times as long. "medium" and "large-v3-turbo" are for a machine with many processors (or patience)
// and half a gigabyte of disk and memory to spare: the turbo is the more exact of the two and about as fast.
var Models = map[string]Model{
	"tiny":   {"tiny", "ggml-tiny-q5_1.bin", 32152673, "818710568da3ca15689e31a743197b520007872ff9576237bda97bd1b469c3d7"},
	"base":   {"base", "ggml-base-q5_1.bin", 59707625, "422f1ae452ade6f30a004d7e5c6a43195e4433bc370bf23fac9cc591f01a8898"},
	"small":  {"small", "ggml-small-q5_1.bin", 190085487, "ae85e4a935d7a567bd102fe55afc16bb595bdb618e11b2fc7591bc08120411bb"},
	"medium": {"medium", "ggml-medium-q5_0.bin", 539212467, "19fea4b380c3a618ec4723c3eef2eb785ffba0d0538cf43f8f235e7b3b34220f"},
	"large-v3-turbo": {"large-v3-turbo", "ggml-large-v3-turbo-q5_0.bin", 574041195,
		"394221709cd5ad1f40c46e6031ca61bce88931e6e088c188294c6d5a55ffa7e2"},
}

const (
	// ModelBase is where the model files are; the file name is appended.
	ModelBase = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/"
	// BinaryBase is where the prebuilt server binaries are (the whisper-server workflow of this repository publishes
	// whisper-server-<os>-<arch> and whisper-server.sha256 under the tag); the file name is appended.
	BinaryBase = "https://github.com/ly-solutions/harnessly/releases/download/whisper-server-" + WhisperVersion + "-r1/"
	// BinarySums names the checksum list published beside the binaries.
	BinarySums = "whisper-server.sha256"
)
