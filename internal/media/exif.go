package media

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"
	_ "golang.org/x/image/webp"
)

// ImageMeta holds all metadata extractable from an image file.
type ImageMeta struct {
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`

	// EXIF fields — all optional; zero values are omitted from JSON.
	TakenAt      *time.Time `json:"taken_at,omitempty"`
	CameraMake   string     `json:"camera_make,omitempty"`
	CameraModel  string     `json:"camera_model,omitempty"`
	LensMake     string     `json:"lens_make,omitempty"`
	LensModel    string     `json:"lens_model,omitempty"`
	Software     string     `json:"software,omitempty"`
	Artist       string     `json:"artist,omitempty"`
	Copyright    string     `json:"copyright,omitempty"`
	Orientation  string     `json:"orientation,omitempty"`
	ISO          int        `json:"iso,omitempty"`
	FNumber      string     `json:"fnumber,omitempty"`
	ExposureTime string     `json:"exposure_time,omitempty"`
	FocalLength  string     `json:"focal_length,omitempty"`
	Flash        string     `json:"flash,omitempty"`
	WhiteBalance string     `json:"white_balance,omitempty"`
	GPSLat       *float64   `json:"gps_lat,omitempty"`
	GPSLon       *float64   `json:"gps_lon,omitempty"`
	GPSAltitude  *float64   `json:"gps_altitude,omitempty"`
}

// ExtractImageMeta reads EXIF and basic image metadata from the file at path.
// Returns an empty struct (no error) for files that have no EXIF data.
func ExtractImageMeta(path string) ImageMeta {
	var meta ImageMeta

	f, err := os.Open(path)
	if err != nil {
		return meta
	}
	defer f.Close()

	// Read pixel dimensions from the image header only (fast — no full decode).
	if cfg, _, err := image.DecodeConfig(f); err == nil {
		meta.Width = cfg.Width
		meta.Height = cfg.Height
	}

	// Reset and decode EXIF.
	if _, err := f.Seek(0, 0); err != nil {
		return meta
	}
	x, err := exif.Decode(f)
	if err != nil {
		return meta // no EXIF — not an error
	}

	meta.CameraMake = exifString(x, exif.Make)
	meta.CameraModel = exifString(x, exif.Model)
	meta.LensMake = exifString(x, exif.LensMake)
	meta.LensModel = exifString(x, exif.LensModel)
	meta.Software = exifString(x, exif.Software)
	meta.Artist = exifString(x, exif.Artist)
	meta.Copyright = exifString(x, exif.Copyright)

	if v := exifString(x, exif.Flash); v != "" {
		meta.Flash = v
	}
	if v := exifString(x, exif.Orientation); v != "" {
		meta.Orientation = orientationLabel(v)
	}
	if v := exifString(x, exif.WhiteBalance); v != "" {
		meta.WhiteBalance = whiteBalanceLabel(v)
	}

	if t, err := x.DateTime(); err == nil {
		meta.TakenAt = &t
	}

	if iso, err := x.Get(exif.ISOSpeedRatings); err == nil {
		if v, err := iso.Int(0); err == nil {
			meta.ISO = v
		}
	}

	if fn, err := x.Get(exif.FNumber); err == nil {
		if num, den, err := fn.Rat2(0); err == nil && den != 0 {
			meta.FNumber = fmt.Sprintf("f/%.1f", float64(num)/float64(den))
		}
	}

	if et, err := x.Get(exif.ExposureTime); err == nil {
		if num, den, err := et.Rat2(0); err == nil && den != 0 {
			if num == 1 {
				meta.ExposureTime = fmt.Sprintf("1/%d s", den)
			} else {
				meta.ExposureTime = fmt.Sprintf("%d/%d s", num, den)
			}
		}
	}

	if fl, err := x.Get(exif.FocalLength); err == nil {
		if num, den, err := fl.Rat2(0); err == nil && den != 0 {
			meta.FocalLength = fmt.Sprintf("%.0f mm", float64(num)/float64(den))
		}
	}

	if lat, lon, err := x.LatLong(); err == nil {
		meta.GPSLat = &lat
		meta.GPSLon = &lon
	}

	if alt, err := x.Get(exif.GPSAltitude); err == nil {
		if num, den, err2 := alt.Rat2(0); err2 == nil && den != 0 {
			v := math.Round(float64(num)/float64(den)*10) / 10
			meta.GPSAltitude = &v
		}
	}

	return meta
}

// ── helpers ───────────────────────────────────────────────────────────────────

func exifString(x *exif.Exif, field exif.FieldName) string {
	tag, err := x.Get(field)
	if err != nil {
		return ""
	}
	s, err := tag.StringVal()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

func orientationLabel(v string) string {
	switch v {
	case "1":
		return "Normal"
	case "2":
		return "Mirrored horizontal"
	case "3":
		return "Rotated 180°"
	case "4":
		return "Mirrored vertical"
	case "5":
		return "Mirrored horizontal, rotated 90° CCW"
	case "6":
		return "Rotated 90° CW"
	case "7":
		return "Mirrored horizontal, rotated 90° CW"
	case "8":
		return "Rotated 90° CCW"
	default:
		return v
	}
}

func whiteBalanceLabel(v string) string {
	switch v {
	case "0":
		return "Auto"
	case "1":
		return "Manual"
	default:
		return v
	}
}
