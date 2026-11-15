// Package assets embeds the favicon, the Fira fonts and the built stylesheet
// (run `make gen` first).
package assets

import "embed"

//go:embed css/output.css favicon.svg fonts
var FS embed.FS
