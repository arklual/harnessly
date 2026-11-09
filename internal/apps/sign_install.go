package apps

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
)

// InstallArchive installs a .hlapp: the archive's signature is checked under the deployment's policy first (VerifyArchive:
// a signature that does not match is always refused, a missing or untrusted one when the deployment requires signatures),
// then the archive is unpacked into a folder of its own and installed from it like any package.
func (r *Registry) InstallArchive(raw []byte, sig *Signature, opt InstallOptions) (Installed, Problems, Verified, error) {
	ver, err := VerifyArchive(raw, sig, currentNetwork())
	if err != nil {
		return Installed{}, nil, ver, err
	}
	tmpRoot := filepath.Join(r.Root, "tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		return Installed{}, nil, ver, err
	}
	work, err := os.MkdirTemp(tmpRoot, "hlapp-")
	if err != nil {
		return Installed{}, nil, ver, err
	}
	defer os.RemoveAll(work)
	dir := filepath.Join(work, "pkg")
	if err := Unpack(raw, dir); err != nil {
		return Installed{}, nil, ver, err
	}
	opt.Signed = ver.Signed
	got, ps, err := r.Install(dir, opt)
	if err != nil {
		return got, ps, ver, err
	}
	return got, ps, ver, nil
}

// publishedArchive is the archive a catalog takes of a published version: the signed .hlapp the version was published
// from (PublishArchive) when the folder still holds exactly its files, else the pack of the folder. What the checks
// looked at (the folder) is what is published: a kept archive that differs from it is not used.
func (r *Registry) publishedArchive(id, version, dir string) ([]byte, string, error) {
	kept := r.VersionDir(id, version) + ".hlapp"
	if sig := sigOf(kept); sig != nil {
		if raw, err := os.ReadFile(kept); err == nil && ArchiveSum(raw) == sig.SHA256 && sameTree(raw, dir) {
			return raw, sig.SHA256, nil
		}
	}
	return Pack(dir)
}

// sameTree says whether the archive holds exactly the files of dir, byte for byte (the manifest as JSON, since publishing
// writes it again).
func sameTree(raw []byte, dir string) bool {
	tmp, err := os.MkdirTemp("", "hlapp-cmp-")
	if err != nil {
		return false
	}
	defer os.RemoveAll(tmp)
	if Unpack(raw, tmp) != nil {
		return false
	}
	files := func(root string) (map[string][]byte, bool) {
		out := map[string][]byte{}
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			b, err := os.ReadFile(p)
			out[filepath.ToSlash(rel)] = b
			return err
		})
		return out, err == nil
	}
	a, ok1 := files(tmp)
	b, ok2 := files(dir)
	if !ok1 || !ok2 || len(a) != len(b) {
		return false
	}
	for name, ab := range a {
		bb, ok := b[name]
		if !ok {
			return false
		}
		if name == ManifestPath {
			var x, y any
			if json.Unmarshal(ab, &x) != nil || json.Unmarshal(bb, &y) != nil || !reflect.DeepEqual(x, y) {
				return false
			}
		} else if !bytes.Equal(ab, bb) {
			return false
		}
	}
	return true
}

// PublishArchive publishes a .hlapp on a channel, the signature checked first as for an install. On stable the published
// version is the archive itself, so the signature is kept with it and travels to the team's catalog (PublishToTeam) and
// to the public one (PublishPublic). A pre-release channel rewrites the version in the manifest, which the signature
// does not cover: that version is published unsigned.
func (r *Registry) PublishArchive(raw []byte, sig *Signature, ch Channel, opt PublishOptions) (PublishedVersion, Problems, Verified, error) {
	ver, err := VerifyArchive(raw, sig, currentNetwork())
	if err != nil {
		return PublishedVersion{}, nil, ver, err
	}
	tmpRoot := filepath.Join(r.Root, "tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		return PublishedVersion{}, nil, ver, err
	}
	work, err := os.MkdirTemp(tmpRoot, "hlapp-")
	if err != nil {
		return PublishedVersion{}, nil, ver, err
	}
	defer os.RemoveAll(work)
	dir := filepath.Join(work, "pkg")
	if err := Unpack(raw, dir); err != nil {
		return PublishedVersion{}, nil, ver, err
	}
	pkg, _ := LoadPackage(dir)
	pv, ps, err := r.Publish(dir, ch, opt)
	if err != nil {
		return pv, ps, ver, err
	}
	if sig != nil && pkg != nil && pkg.Manifest.Version == pv.Version {
		kept := r.VersionDir(pv.ID, pv.Version) + ".hlapp"
		if err := writeFileAtomic(kept, raw); err == nil {
			keepSig(kept, raw, sig)
		}
	}
	return pv, ps, ver, nil
}

// versionSigned says whether a published version is, byte for byte, an archive a trusted publisher signed (PublishArchive
// kept it), so that installing it from its channel is installing the signed package.
func (r *Registry) versionSigned(id, version string) bool {
	kept := r.VersionDir(id, version) + ".hlapp"
	sig := sigOf(kept)
	raw, err := os.ReadFile(kept)
	if sig == nil || err != nil || !sameTree(raw, r.VersionDir(id, version)) {
		return false
	}
	v, err := VerifyArchive(raw, sig, currentNetwork())
	return err == nil && v.Signed
}
