// Package web provides the embedded web assets (templates + static files).
package web

import (
	"embed"
	"io/fs"
	"log/slog"
)

//go:embed templates static
var assets embed.FS

// Templates returns the sub-filesystem rooted at "templates".
func Templates() fs.FS {
	sub, err := fs.Sub(assets, "templates")
	if err != nil {
		slog.Error("web: sub templates", "error", err)
	}
	return sub
}

// Static returns the sub-filesystem rooted at "static".
func Static() fs.FS {
	sub, err := fs.Sub(assets, "static")
	if err != nil {
		slog.Error("web: sub static", "error", err)
	}
	return sub
}
