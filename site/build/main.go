// Command build renders the website to site/dist: index.html from the templ page, the app's
// fonts and icons, and the CNAME for GitHub Pages. The stylesheet is built by `make site`
// (Tailwind) before this runs.
package main

import (
	"context"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"yasssm/assets"
	"yasssm/site"
)

const (
	dist   = "site/dist"
	domain = "yasssm.luizlab.com.br"
)

func main() {
	f, err := os.Create(filepath.Join(dist, "index.html"))
	if err != nil {
		log.Fatal(err)
	}
	if err := site.Page().Render(context.Background(), f); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "CNAME"), []byte(domain+"\n"), 0o644); err != nil {
		log.Fatal(err)
	}
	// Same embedded files the app serves, minus its stylesheet.
	err = fs.WalkDir(assets.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) == ".css" || path == "manifest.json" {
			return err
		}
		b, err := fs.ReadFile(assets.FS, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dist, "assets", path)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
	if err != nil {
		log.Fatal(err)
	}
}
