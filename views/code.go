package views

import (
	"context"
	"html"
	"io"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"

	"yasssm/utils"
)

// Code blocks are highlighted on the server with chroma, so pages still carry
// no JavaScript. The formatter emits chroma's short token classes (k, s, c…),
// which globals.css colors from the theme in light and dark alike.

// codeClass sizes the text by screen: 11px on phones, 12px on tablets, 13px
// on desktops. Long lines wrap anywhere instead of scrolling sideways.
const codeClass = "code max-h-[75vh] overflow-auto rounded-lg border bg-muted/40 p-2 font-mono text-[0.6875rem] leading-relaxed whitespace-pre-wrap [overflow-wrap:anywhere] sm:p-3 sm:text-xs lg:text-[0.8125rem]"

var (
	bashLexer    = lexers.Get("bash")
	consoleLexer = lexers.Get("console")
)

// crontabLexer marks the schedule of each entry and hands the command to the
// shell lexer. A system crontab's user column reads as the command's first word.
var crontabLexer = chroma.MustNewLexer(&chroma.Config{Name: "crontab"}, func() chroma.Rules {
	return chroma.Rules{
		"root": {
			{Pattern: `^[ \t]*#.*\n?`, Type: chroma.CommentSingle},
			{Pattern: `^([ \t]*)([A-Za-z_]\w*)([ \t]*=[ \t]*)(.*\n?)`, Type: chroma.ByGroups(chroma.TextWhitespace, chroma.NameVariable, chroma.Operator, chroma.LiteralString)},
			{Pattern: `^([ \t]*)(@\w+|(?:\S+[ \t]+){4}\S+)([ \t]+)`, Type: chroma.ByGroups(chroma.TextWhitespace, chroma.Keyword, chroma.TextWhitespace), Mutator: chroma.Push("command")},
			{Pattern: `.+\n?|\n`, Type: chroma.Text},
		},
		"command": {
			{Pattern: `.+\n?|\n`, Type: chroma.UsingLexer(bashLexer), Mutator: chroma.Pop(1)},
		},
	}
})

// logLexer is for container logs and job output: the leading time, log
// levels, quoted strings, logfmt keys and `$ command` lines. Warnings use
// the traceback class (gt), which no other lexer here emits.
var logLexer = chroma.MustNewLexer(&chroma.Config{Name: "log"}, func() chroma.Rules {
	return chroma.Rules{
		"root": {
			{Pattern: `^(\$ )(.*\n?)`, Type: chroma.ByGroups(chroma.GenericPrompt, chroma.UsingLexer(bashLexer))},
			{Pattern: `^(?:[A-Z][a-z]{2} \d\d )?\d\d:\d\d:\d\d`, Type: chroma.LiteralDate},
			{Pattern: `(?i)\b(?:fatal|panic|emerg(?:ency)?|crit(?:ical)?|err(?:or)?|fail(?:ed|ure|s)?|exception)\b`, Type: chroma.GenericError},
			{Pattern: `(?i)\b(?:warn(?:ing)?|deprecated)\b`, Type: chroma.GenericTraceback},
			{Pattern: `(?i)\b(?:debug|trace)\b`, Type: chroma.CommentSingle},
			{Pattern: `"(?:\\.|[^"\\\n])*"`, Type: chroma.LiteralStringDouble},
			{Pattern: `[\w.-]+(?==)`, Type: chroma.NameAttribute},
			{Pattern: `\w+|\s+|.`, Type: chroma.Text},
		},
	}
})

// sourceLexer picks a lexer for a script or config file by its path, then
// by its content (a shebang), else plain text.
func sourceLexer(path, text string) chroma.Lexer {
	if path == "/etc/crontab" || strings.HasPrefix(path, "/etc/cron.d/") {
		return crontabLexer
	}
	if l := lexers.Match(filepath.Base(path)); l != nil {
		return l
	}
	if l := lexers.Analyse(text); l != nil {
		return l
	}
	return lexers.Fallback
}

// codeBlock renders text highlighted by l in a <pre>; numbered adds line
// numbers and class is merged into the block's classes.
func codeBlock(text string, l chroma.Lexer, numbered bool, class string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		it, err := chroma.Coalesce(l).Tokenise(nil, text)
		if err != nil {
			// Highlighting is cosmetic: show the text unhighlighted.
			it, _ = lexers.Fallback.Tokenise(nil, text)
		}
		f := chromahtml.New(
			chromahtml.WithClasses(true),
			chromahtml.WithLineNumbers(numbered),
			chromahtml.WithPreWrapper(codePre(utils.CN(codeClass, class))),
		)
		return f.Format(w, styles.Fallback, it)
	})
}

// codePre replaces chroma's <pre class="chroma"> with the yasssm classes.
type codePre string

func (c codePre) Start(code bool, _ string) string {
	return `<pre class="` + html.EscapeString(string(c)) + `"><code>`
}

func (codePre) End(code bool) string { return "</code></pre>" }

// ansiEscape matches terminal color and cursor sequences that some
// containers write even without a TTY.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// logText joins log lines prefixed by their time in server time: only the
// clock when one day is shown, else month, day and clock.
func logText(d Logs) string {
	layout := "Jan 02 15:04:05"
	if d.Day != "" {
		layout = time.TimeOnly
	}
	var b strings.Builder
	for _, l := range d.Lines {
		if !l.Time.IsZero() {
			b.WriteString(l.Time.Local().Format(layout))
			b.WriteByte(' ')
		}
		b.WriteString(ansiEscape.ReplaceAllString(l.Text, ""))
		b.WriteByte('\n')
	}
	return b.String()
}

// logsURL is the logs page of the same container and line count for day
// ("" for the latest lines of any day).
func logsURL(d Logs, day string) string {
	q := url.Values{"n": {strconv.Itoa(d.N)}}
	if day != "" {
		q.Set("day", day)
	}
	return d.Container.LogsURL + "?" + q.Encode()
}

// shiftDay moves a YYYY-MM-DD day by n days.
func shiftDay(day string, n int) string {
	t, err := time.ParseInLocation(time.DateOnly, day, time.Local)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, n).Format(time.DateOnly)
}

// logsSummary says which lines are shown and in which time zone.
func logsSummary(d Logs) string {
	s := "Last " + strconv.Itoa(len(d.Lines)) + " lines"
	if t, err := time.ParseInLocation(time.DateOnly, d.Day, time.Local); err == nil {
		s += " from " + t.Format("Mon, Jan 2")
	}
	return s + " · times in " + time.Now().Format("MST")
}
