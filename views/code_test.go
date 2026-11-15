package views

import (
	"context"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
)

// Container logs and scripts are untrusted and rendered without templ's
// escaping, so the highlighter must escape them.
func TestCodeBlockEscapes(t *testing.T) {
	for name, c := range map[string]struct {
		text  string
		lexer chroma.Lexer
	}{
		"log":     {`level=error msg="<script>alert(1)</script>"`, logLexer},
		"crontab": {"* * * * * root echo '<img src=x onerror=alert(1)>'\n", crontabLexer},
	} {
		var b strings.Builder
		if err := codeBlock(c.text, c.lexer, true, "").Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		if out := b.String(); strings.Contains(out, "<script") || strings.Contains(out, "<img") {
			t.Errorf("%s: markup not escaped: %s", name, out)
		}
	}
}
