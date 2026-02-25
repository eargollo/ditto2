package api

import (
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
)

// newPageHandler returns a factory that creates an http.HandlerFunc rendering
// the named template using base.html as the layout. The FS is the templates FS.
func newPageHandler(templatesFS fs.FS) func(name string) http.HandlerFunc {
	return func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			tmpl, err := template.ParseFS(templatesFS, "base.html", name)
			if err != nil {
				http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if err := tmpl.ExecuteTemplate(w, "base", nil); err != nil {
				slog.Error("template execute", "name", name, "error", err)
			}
		}
	}
}
