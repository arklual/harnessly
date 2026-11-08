package filesync

// Codec turns a local file into the collection's canonical form and back. The canonical form is what machines compare
// and what the store keeps. A Sinopia library is Plain: files as they are. The seam stays so that a collection that
// carries machine paths or is sealed before it leaves the machine can be added without touching the sync.
type Codec interface {
	// Canonical is the shareable form of a local file.
	Canonical(rel string, local []byte) []byte
	// Local is the file this machine writes for canonical contents; current is what the file holds now (nil when
	// absent), so machine-only parts survive.
	Local(rel string, canonical, current []byte) []byte
	// Tag fingerprints canonical contents; equal tags mean equal contents.
	Tag(canonical []byte) string
	// Seal makes the blob the hub stores; Open reverses it.
	Seal(rel string, canonical []byte) ([]byte, error)
	Open(rel string, blob []byte) ([]byte, error)
	// Merge combines two edits of the same base, true when it could (settings merge key by key).
	Merge(rel string, base, local, remote []byte, localNewer bool) ([]byte, bool)
	// Ready tells whether the codec can work (a sealed collection waits for its key).
	Ready() bool
}

// Plain is the codec of files as they are.
type Plain struct{}

func (Plain) Canonical(_ string, b []byte) []byte                       { return b }
func (Plain) Local(_ string, c, _ []byte) []byte                        { return c }
func (Plain) Tag(c []byte) string                                       { return sha(c) }
func (Plain) Seal(_ string, c []byte) ([]byte, error)                   { return c, nil }
func (Plain) Open(_ string, b []byte) ([]byte, error)                   { return b, nil }
func (Plain) Merge(string, []byte, []byte, []byte, bool) ([]byte, bool) { return nil, false }
func (Plain) Ready() bool                                               { return true }
