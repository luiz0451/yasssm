// Package assets embeds the favicon and the built stylesheet (run `make gen` first).
package assets

import "embed"

//go:embed css/output.css favicon.svg
var FS embed.FS
