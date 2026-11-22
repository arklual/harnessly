package imagegen

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// addPicture publishes an imported picture through the same job/copy flow for every provider.
func (s *Service) addPicture(j *job, r Request, dst string, n int) bool {
	if wrong := finishEdit(r, dst); wrong != "" {
		os.Remove(dst)
		s.update(j, func(sn *Snapshot) { sn.Error = wrong })
		return false
	}
	im := Image{N: n, Path: dst}
	if r.CopyTo != "" {
		name := r.Name
		if name == "" {
			name = "image-" + j.snap.ID[:6]
		}
		base := fmt.Sprintf("%s-%d%s", name, n, filepath.Ext(dst))
		if dir, ok := saveDir(r.CopyTo, r.SaveOK); ok && copyFile(dst, filepath.Join(dir, base)) == nil {
			im.Copy = filepath.Join(dir, base)
		}
	}
	s.update(j, func(sn *Snapshot) { sn.Images = append(sn.Images, im) })
	return true
}

// Antigravity writes into its session's artifact directory, after the parent turn has ended.
// Never import paths mentioned in model text, follow a session-directory symlink, or publish a
// partial/invalid image. Decode the bounded bytes we actually opened, then atomically store those
// same bytes under the job; the file viewer needs no access to Antigravity's credential folders.
func (s *Service) antigravityCollector(j *job, r Request, out string) func(string) int {
	seen := map[string]bool{}
	count := 0
	return func(dir string) int {
		st, err := os.Lstat(dir)
		if err != nil || !st.IsDir() {
			return count
		}
		realDir, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return count
		}
		for _, path := range newPictures(dir, seen) {
			if count >= r.Count {
				break
			}
			in, st, err := openChecked(path, func(real string) bool { return filepath.Dir(real) == realDir })
			if err != nil {
				continue
			}
			if st.Size() > maxSource {
				in.Close()
				continue
			}
			raw, err := io.ReadAll(io.LimitReader(in, maxSource+1))
			in.Close()
			if err != nil || len(raw) > maxSource {
				continue
			}
			if _, err := decodeBounded(raw, "generated image"); err != nil {
				continue
			}
			n := count + 1
			dst := filepath.Join(out, fmt.Sprintf("%d%s", n, strings.ToLower(filepath.Ext(path))))
			if err := replaceFile(dst, raw); err != nil {
				continue
			}
			if s.addPicture(j, r, dst, n) {
				seen[path] = true
				count++
			}
		}
		return count
	}
}
