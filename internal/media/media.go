package media

import (
	"bytes"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
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

// ContentType returns the MIME content type for the file based on its extension.
// Returns "application/octet-stream" for unknown types.
func ContentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		return "application/octet-stream"
	}
	return ct
}

// Thumbnail generates a JPEG thumbnail for the image at path, resized to fit
// within width x height while preserving the aspect ratio.
// Returns nil, nil for non-image files or unsupported formats (video, etc.).
// The output is always JPEG at quality 75.
func Thumbnail(path string, width, height int) ([]byte, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if !imageExts[ext] {
		return nil, nil
	}

	// We only decode formats we have pure-Go decoders for.
	// heic/heif/avif/bmp/tiff require CGo or additional libraries — skip.
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		// supported below
	default:
		return nil, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	src, err := decodeImage(ext, f)
	if err != nil {
		// Treat decode errors as "can't thumbnail" rather than hard errors.
		return nil, nil
	}

	thumb := resizeFit(src, width, height)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, thumb, &jpeg.Options{Quality: 75}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decodeImage decodes an image from r using the decoder appropriate for ext.
func decodeImage(ext string, r io.Reader) (image.Image, error) {
	switch ext {
	case ".jpg", ".jpeg":
		return jpeg.Decode(r)
	case ".png":
		return png.Decode(r)
	case ".gif":
		return gif.Decode(r)
	case ".webp":
		return webp.Decode(r)
	default:
		img, _, err := image.Decode(r)
		return img, err
	}
}

// resizeFit scales src to fit within the dstW x dstH bounding box,
// preserving the aspect ratio, using BiLinear interpolation.
func resizeFit(src image.Image, dstW, dstH int) image.Image {
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	if srcW == 0 || srcH == 0 {
		return src
	}

	// Compute scale factor to fit within the box.
	scaleW := float64(dstW) / float64(srcW)
	scaleH := float64(dstH) / float64(srcH)
	scale := scaleW
	if scaleH < scale {
		scale = scaleH
	}

	// No upscaling — if the image already fits, return as-is.
	if scale >= 1.0 {
		return src
	}

	newW := int(float64(srcW) * scale)
	newH := int(float64(srcH) * scale)
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.BiLinear.Scale(dst, dst.Bounds(), src, srcBounds, draw.Over, nil)
	return dst
}
