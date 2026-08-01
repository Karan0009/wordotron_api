// Package storage abstracts object storage behind a small interface so the
// business logic never learns whether a file lives on disk, in MinIO or in S3.
// The provider is chosen once, at start-up, from configuration.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Karan0009/wordotron_api/internal/config"
)

// Object describes a stored file.
type Object struct {
	Key         string    `json:"key"`
	URL         string    `json:"url"`
	Size        int64     `json:"size"`
	ContentType string    `json:"content_type"`
	UploadedAt  time.Time `json:"uploaded_at"`
}

// Storage is the object storage contract. Keys are provider-agnostic,
// slash-separated paths such as "avatars/<uuid>.png".
type Storage interface {
	// Put stores the contents of r under key. size may be -1 when unknown.
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) (*Object, error)
	// Get opens the object for reading. The caller must close it.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes the object. Deleting a missing key is not an error.
	Delete(ctx context.Context, key string) error
	// Exists reports whether the object is present.
	Exists(ctx context.Context, key string) (bool, error)
	// URL returns the publicly reachable address for key.
	URL(key string) string
}

// New builds the configured Storage implementation.
func New(ctx context.Context, cfg config.Storage, log *slog.Logger) (Storage, error) {
	switch strings.ToLower(cfg.Provider) {
	case "local":
		return NewLocal(cfg, log)
	case "s3":
		return NewS3(ctx, cfg, log)
	default:
		return nil, fmt.Errorf("storage: unknown provider %q", cfg.Provider)
	}
}

// BuildKey produces a collision-free key that preserves the original
// extension, e.g. BuildKey("avatars", "me.PNG") -> "avatars/<uuid>.png".
func BuildKey(prefix, filename string) string {
	ext := strings.ToLower(path.Ext(filename))
	if len(ext) > 10 {
		ext = ""
	}
	name := uuid.NewString() + ext
	if prefix == "" {
		return name
	}
	return path.Join(cleanPrefix(prefix), name)
}

func cleanPrefix(prefix string) string {
	return strings.Trim(path.Clean("/"+prefix), "/")
}

// validateKey rejects keys that could escape the storage root.
func validateKey(key string) error {
	if key == "" {
		return fmt.Errorf("storage: empty key")
	}
	if strings.Contains(key, "..") || strings.HasPrefix(key, "/") || strings.Contains(key, `\`) {
		return fmt.Errorf("storage: invalid key %q", key)
	}
	return nil
}

// ErrNotFound is returned by Get when the key does not exist.
var ErrNotFound = errors.New("storage: object not found")
