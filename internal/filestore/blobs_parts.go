package filestore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/minio/minio-go/v7"
)

// An upload in parts is put together where its blob will be, as the parts come, never copied once more:
//
//   - a folder writes each part at its place in uploads/<room>/<upload> (beside the blobs, on the same disk), and
//     finishing truncates that file to the size and renames it to the blob;
//   - a bucket opens a multipart upload of the blob's key, each part is a part of it, and finishing completes it (the
//     bucket joins the parts; nothing passes through the hub).
//
// Neither is a blob before it is finished: a listing of the blobs (and so the gc) never sees an upload under way.

// uploadsDir is where a folder keeps the uploads of a room under way.
func (f *fileBlobs) uploadPath(room, upload string) string {
	return filepath.Join(f.root, "uploads", room, upload)
}

func (f *fileBlobs) UploadBegin(_ context.Context, room, upload, sha string) (string, error) {
	if !ValidUploadID(upload) || !ValidSha(sha) {
		return "", fmt.Errorf("%w: upload", ErrBadRequest)
	}
	p := f.uploadPath(room, upload)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	file, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	return "", file.Close()
}

func (f *fileBlobs) UploadWrite(_ context.Context, room string, u *Upload, _ int, off, size int64, r io.Reader) (string, error) {
	if !ValidUploadID(u.ID) {
		return "", fmt.Errorf("%w: upload", ErrBadRequest)
	}
	file, err := os.OpenFile(f.uploadPath(room, u.ID), os.O_WRONLY, 0)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(io.NewOffsetWriter(file, off), io.LimitReader(r, size))
	if err == nil {
		err = file.Sync()
	}
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	return "", err
}

func (f *fileBlobs) UploadFinish(_ context.Context, room string, u *Upload) error {
	if !ValidUploadID(u.ID) || !ValidSha(u.Sha) {
		return fmt.Errorf("%w: upload", ErrBadRequest)
	}
	src := f.uploadPath(room, u.ID)
	file, err := os.OpenFile(src, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	// A part refused after it was written may have left bytes past the end.
	err = file.Truncate(u.Size)
	if err == nil {
		err = file.Sync()
	}
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	dst := f.path(room, u.Sha)
	if _, err := os.Stat(dst); err == nil {
		return os.Remove(src)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

func (f *fileBlobs) UploadAbort(_ context.Context, room string, u *Upload) error {
	if !ValidUploadID(u.ID) {
		return nil
	}
	if err := os.Remove(f.uploadPath(room, u.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// FreeSpace is how many bytes the folder's disk still takes; ok is false where the system does not tell.
func (f *fileBlobs) FreeSpace() (int64, bool) { return freeSpace(f.root) }

// spaceReporter is a store on a disk of its own size (a folder); a bucket has none.
type spaceReporter interface {
	FreeSpace() (free int64, ok bool)
}

func (s *s3Blobs) core() minio.Core { return minio.Core{Client: s.c} }

func (s *s3Blobs) UploadBegin(ctx context.Context, room, upload, sha string) (string, error) {
	if !ValidUploadID(upload) || !ValidSha(sha) {
		return "", fmt.Errorf("%w: upload", ErrBadRequest)
	}
	return s.core().NewMultipartUpload(ctx, s.bucket, s.key(room, sha), minio.PutObjectOptions{ContentType: "application/octet-stream"})
}

func (s *s3Blobs) UploadWrite(ctx context.Context, room string, u *Upload, n int, _, size int64, r io.Reader) (string, error) {
	if u.Handle == "" || !ValidSha(u.Sha) {
		return "", fmt.Errorf("%w: upload", ErrBadRequest)
	}
	p, err := s.core().PutObjectPart(ctx, s.bucket, s.key(room, u.Sha), u.Handle, n+1, r, size, minio.PutObjectPartOptions{})
	if noUpload(err) {
		return "", os.ErrNotExist
	}
	return p.ETag, err
}

func (s *s3Blobs) UploadFinish(ctx context.Context, room string, u *Upload) error {
	if u.Handle == "" || !ValidSha(u.Sha) {
		return fmt.Errorf("%w: upload", ErrBadRequest)
	}
	parts := make([]minio.CompletePart, 0, len(u.Parts))
	for _, p := range u.Parts {
		parts = append(parts, minio.CompletePart{PartNumber: p.N + 1, ETag: p.Tag})
	}
	_, err := s.core().CompleteMultipartUpload(ctx, s.bucket, s.key(room, u.Sha), u.Handle, parts, minio.PutObjectOptions{})
	if err != nil && noUpload(err) {
		// Finished by an earlier try whose answer was lost: the blob is there.
		if ok, herr := s.Has(ctx, room, u.Sha); herr == nil && ok {
			return nil
		}
	}
	return err
}

func (s *s3Blobs) UploadAbort(ctx context.Context, room string, u *Upload) error {
	if u.Handle == "" || !ValidSha(u.Sha) {
		return nil
	}
	err := s.core().AbortMultipartUpload(ctx, s.bucket, s.key(room, u.Sha), u.Handle)
	if err != nil && (noUpload(err) || notFound(err)) {
		return nil
	}
	return err
}

// noUpload is the bucket not knowing a multipart upload (finished, aborted or expired by the bucket's own rules).
func noUpload(err error) bool {
	return err != nil && minio.ToErrorResponse(err).Code == "NoSuchUpload"
}
