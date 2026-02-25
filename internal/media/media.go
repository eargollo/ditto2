package media

import (
	"path/filepath"
	"strings"
)

// FileType classifies a file for grouping and display.
type FileType string

const (
	FileTypeImage    FileType = "image"
	FileTypeVideo    FileType = "video"
	FileTypeDocument FileType = "document"
	FileTypeOther    FileType = "other"
)

var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
	".bmp": true, ".webp": true, ".tiff": true, ".tif": true,
	".heic": true, ".heif": true, ".avif": true,
}

var videoExts = map[string]bool{
	".mp4": true, ".mov": true, ".avi": true, ".mkv": true,
	".wmv": true, ".flv": true, ".webm": true, ".m4v": true,
}

var documentExts = map[string]bool{
	".pdf": true, ".doc": true, ".docx": true, ".xls": true,
	".xlsx": true, ".ppt": true, ".pptx": true, ".txt": true,
	".odt": true, ".ods": true, ".odp": true,
}

// Detect returns the FileType for the given file path based on extension.
func Detect(path string) FileType {
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case imageExts[ext]:
		return FileTypeImage
	case videoExts[ext]:
		return FileTypeVideo
	case documentExts[ext]:
		return FileTypeDocument
	default:
		return FileTypeOther
	}
}

// Thumbnail generates a JPEG thumbnail for the file at path and returns the bytes.
// Stub — not yet implemented.
func Thumbnail(path string) ([]byte, error) {
	return nil, errNotImplemented("Thumbnail")
}

func errNotImplemented(what string) error {
	return &notImplementedError{what: what}
}

type notImplementedError struct{ what string }

func (e *notImplementedError) Error() string {
	return e.what + ": not yet implemented"
}
