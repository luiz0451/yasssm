// Command build renders the website to site/dist: the home and docs pages, the code
// highlighting stylesheet, the app's fonts and icons, and the CNAME for GitHub Pages. The
// main stylesheet is built by `make site` (Tailwind) before this runs.
package main

import (
	"context"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/a-h/templ"

	"yasssm/assets"
	"yasssm/site"
)

const (
	dist   = "site/dist"
	domain = "yasssm.luizlab.com.br"
)

func main() {
	render("index.html", site.Page())
	render("docs.html", site.Docs())
	css, err := site.HighlightCSS()
	if err != nil {
		log.Fatal(err)
	}
	write("assets/css/highlight.css", []byte(css))
	write("CNAME", []byte(domain+"\n"))
	// The one-line installers: curl -fsSL https://yasssm.luizlab.com.br/install.sh | sh
	for _, name := range []string{"install.sh", "install-macos.sh"} {
		b, err := os.ReadFile(filepath.Join("site", name))
		if err != nil {
			log.Fatal(err)
		}
		write(name, b)
	}
	// The app's fonts and icons; its own stylesheet and manifest are not needed.
	err = fs.WalkDir(assets.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == "manifest.json" || filepath.Ext(path) == ".css" {
			return err
		}
		b, err := fs.ReadFile(assets.FS, path)
		if err != nil {
			return err
		}
		write(filepath.Join("assets", path), b)
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
}

func render(name string, c templ.Component) {
	f, err := os.Create(filepath.Join(dist, name))
	if err != nil {
		log.Fatal(err)
	}
	if err := c.Render(context.Background(), f); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}

func write(name string, b []byte) {
	out := filepath.Join(dist, name)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(out, b, 0o644); err != nil {
		log.Fatal(err)
	}
}
