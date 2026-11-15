// Package assets embeds the favicon, the home-screen icons and web manifest, the fonts and the
// built stylesheet (run `make gen` first).
package assets

import "embed"

//go:embed css/output.css favicon.svg icons manifest.json fonts
var FS embed.FS
