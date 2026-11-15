package site

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/a-h/templ"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// Code blocks are highlighted when the site is built (the site runs no JavaScript), with
// CSS classes; HighlightCSS holds the colours for the light and dark themes.

var formatter = html.New(html.WithClasses(true), html.PreventSurroundingPre(true))

// highlight renders code as highlighted HTML spans for the given chroma language ("bash").
// For shell, the command at the start of each line and its -flags get their own colours too;
// chroma's bash lexer leaves them as plain text.
func highlight(code, lang string) templ.Component {
	lexer := lexers.Get(lang)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	it, err := chroma.Coalesce(lexer).Tokenise(nil, code)
	var b bytes.Buffer
	if err == nil {
		tokens := it.Tokens()
		if lang == "bash" {
			tokens = markCommands(tokens)
		}
		err = formatter.Format(&b, styles.Get("github"), chroma.Literator(tokens...))
	}
	if err != nil {
		return templ.Raw(templ.EscapeString(code))
	}
	return templ.Raw(b.String())
}

var word = regexp.MustCompile(`\s+|\S+`)

// markCommands splits plain-text tokens into words: a line's first word becomes a function
// name (the command), a word starting with "-" a constant (a flag; both styles colour it).
func markCommands(tokens []chroma.Token) []chroma.Token {
	var out []chroma.Token
	lineStart := true
	for _, t := range tokens {
		if t.Type != chroma.Text && t.Type != chroma.NameBuiltin {
			out = append(out, t)
			lineStart = strings.HasSuffix(t.Value, "\n")
			continue
		}
		for _, w := range word.FindAllString(t.Value, -1) {
			typ := t.Type
			switch {
			case strings.TrimSpace(w) == "":
				typ = chroma.Text
			case lineStart:
				typ = chroma.NameFunction
			case strings.HasPrefix(w, "-"):
				typ = chroma.NameConstant
			}
			out = append(out, chroma.Token{Type: typ, Value: w})
			if strings.TrimSpace(w) != "" {
				lineStart = false
			} else if strings.Contains(w, "\n") {
				lineStart = true
			}
		}
	}
	return out
}

// HighlightCSS is the stylesheet for highlighted code: github in light mode, github-dark in
// dark mode, without the styles' own backgrounds (the code block sets it).
func HighlightCSS() (string, error) {
	var light, dark bytes.Buffer
	if err := formatter.WriteCSS(&light, styles.Get("github")); err != nil {
		return "", err
	}
	if err := formatter.WriteCSS(&dark, styles.Get("github-dark")); err != nil {
		return "", err
	}
	// Each style in its own media query: a class only one style defines must not leak into
	// the other theme (github's dark .na text on github-dark's background).
	return "@media (prefers-color-scheme: light) {\n" + dropBackground(light.String()) + "}\n" +
		"@media (prefers-color-scheme: dark) {\n" + dropBackground(dark.String()) + "}\n", nil
}

// dropBackground removes the style's own background rules; the code block sets its own.
func dropBackground(css string) string {
	var keep []string
	for line := range strings.Lines(css) {
		if !strings.HasPrefix(line, "/* Background */") && !strings.HasPrefix(line, "/* PreWrapper */") {
			keep = append(keep, line)
		}
	}
	return strings.Join(keep, "")
}
