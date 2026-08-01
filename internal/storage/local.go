package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Karan0009/wordotron_api/internal/config"
)

// Local stores objects on the filesystem. It is intended for development and
// single-node deployments; use the S3 provider behind a load balancer.
type Local struct {
	root    string
	baseURL string
	log     *slog.Logger
}

var _ Storage = (*Local)(nil)

// NewLocal creates the root directory if needed and returns the provider.
func NewLocal(cfg config.Storage, log *slog.Logger) (*Local, error) {
	root, err := filepath.Abs(cfg.LocalPath)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve local path: %w", err)
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("storage: create local root: %w", err)
	}

	log.Info("using local object storage", slog.String("root", root))
	return &Local{
		root:    root,
		baseURL: strings.TrimRight(cfg.PublicBaseURL, "/"),
		log:     log,
	}, nil
}

func (l *Local) Put(_ context.Context, key string, r io.Reader, _ int64, contentType string) (*Object, error) {
	fullPath, err := l.resolve(key)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
		return nil, fmt.Errorf("storage: create directory: %w", err)
	}

	// Write to a temp file and rename so a failed upload never leaves a
	// truncated object behind.
	tmp, err := os.CreateTemp(filepath.Dir(fullPath), ".upload-*")
	if err != nil {
		return nil, fmt.Errorf("storage: create temp file: %w", err)
	}
	tmpName := tmp.Name()

	written, copyErr := io.Copy(tmp, r)
	closeErr := tmp.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tmpName)
		return nil, fmt.Errorf("storage: write object: %w", errors.Join(copyErr, closeErr))
	}

	if err := os.Rename(tmpName, fullPath); err != nil {
		_ = os.Remove(tmpName)
		return nil, fmt.Errorf("storage: finalise object: %w", err)
	}
	if err := os.Chmod(fullPath, 0o640); err != nil {
		return nil, fmt.Errorf("storage: set permissions: %w", err)
	}

	return &Object{
		Key:         key,
		URL:         l.URL(key),
		Size:        written,
		ContentType: contentType,
		UploadedAt:  time.Now().UTC(),
	}, nil
}

func (l *Local) Get(_ context.Context, key string) (io.ReadCloser, error) {
	fullPath, err := l.resolve(key)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(fullPath) //nolint:gosec // path is validated by resolve
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: open object: %w", err)
	}
	return file, nil
}

func (l *Local) Delete(_ context.Context, key string) error {
	fullPath, err := l.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage: delete object: %w", err)
	}
	return nil
}

func (l *Local) Exists(_ context.Context, key string) (bool, error) {
	fullPath, err := l.resolve(key)
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(fullPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("storage: stat object: %w", err)
	}
	return true, nil
}

func (l *Local) URL(key string) string {
	return l.baseURL + "/" + strings.TrimPrefix(key, "/")
}

// Root exposes the base directory so the HTTP layer can serve files from it.
func (l *Local) Root() string { return l.root }

// resolve validates the key and confirms the result stays inside the root.
func (l *Local) resolve(key string) (string, error) {
	if err := validateKey(key); err != nil {
		return "", err
	}

	fullPath := filepath.Join(l.root, filepath.FromSlash(key))
	if !strings.HasPrefix(fullPath, l.root+string(os.PathSeparator)) {
		return "", fmt.Errorf("storage: key escapes root: %q", key)
	}
	return fullPath, nil
}
