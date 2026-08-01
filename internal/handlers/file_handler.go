package handlers

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/Karan0009/wordotron_api/internal/storage"
	"github.com/Karan0009/wordotron_api/pkg/apperror"
)

// FileHandler streams objects out of the configured storage provider. It is
// only mounted for the local provider; with S3 or MinIO the SPA fetches the
// object URL directly and the API never sits in the data path.
type FileHandler struct {
	files storage.Storage
}

// NewFileHandler builds the file handler.
func NewFileHandler(files storage.Storage) *FileHandler {
	return &FileHandler{files: files}
}

// Serve streams a stored object.
//
//	@Summary		Download an uploaded file
//	@Tags			files
//	@Produce		octet-stream
//	@Param			key	path	string	true	"Storage key, e.g. avatars/<uuid>.png"
//	@Success		200
//	@Failure		404	{object}	utils.ErrorEnvelope
//	@Router			/files/{key} [get]
func (h *FileHandler) Serve(c fiber.Ctx) error {
	key := strings.TrimPrefix(c.Params("*"), "/")
	if key == "" {
		return apperror.NotFound("File")
	}

	reader, err := h.files.Get(c.Context(), key)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return apperror.NotFound("File")
		}
		return apperror.BadRequest("Invalid file key").Wrap(err)
	}
	defer func() { _ = reader.Close() }()

	// Uploads are user-controlled bytes: never let a browser sniff them into
	// an executable content type.
	c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
	c.Set(fiber.HeaderCacheControl, "public, max-age=86400")
	c.Set(fiber.HeaderContentDisposition, "inline")

	return c.SendStream(reader)
}
