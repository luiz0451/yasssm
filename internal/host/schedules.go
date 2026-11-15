package host

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Scheduled things on the host, read only: systemd timers (with the command and, when it is a
// script, the script itself) and the system cron files and the scripts they run.

const sourceCap = 64 << 10 // larger files are not inlined

// Source is the code behind a scheduled entry: a script file or a prompt.
type Source struct {
	Path, Text, Note string // Note says why Text is empty (binary, too large, unreadable)
}

type Timer struct {
	Unit, Activates string
	Schedule        []string // e.g. "OnCalendar=*-*-* 06:00:00", "OnUnitActiveSec=10min"
	Next, Last      string
	Command         string
	Sources         []Source
}

var (
	timerSpec  = regexp.MustCompile(`\{ ([A-Za-z]+)=([^;]*?) ;`)
	execArgv   = regexp.MustCompile(`argv\[\]=([^;]*?) ;`)
	usecSuffix = regexp.MustCompile(`USec$`)
)

// ListTimers lists the system or user systemd timers with their schedule and command.
func ListTimers(ctx context.Context, user bool) ([]Timer, error) {
	sc := func(args ...string) []string {
		if user {
			return append([]string{"--user"}, args...)
		}
		return args
	}
	out, err := run(ctx, "systemctl", sc("list-timers", "--all", "--output=json", "--no-pager")...)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Unit, Activates string
		Next, Last      uint64 // µs since the epoch, 0 = n/a
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, err
	}
	timers := make([]Timer, 0, len(raw))
	for _, r := range raw {
		t := Timer{Unit: r.Unit, Activates: r.Activates, Next: usecTime(r.Next), Last: usecTime(r.Last)}
		props, _ := run(ctx, "systemctl", sc("show", r.Unit, "-p", "TimersCalendar", "-p", "TimersMonotonic")...)
		for line := range strings.Lines(props) {
			for _, m := range timerSpec.FindAllStringSubmatch(line, -1) {
				t.Schedule = append(t.Schedule, usecSuffix.ReplaceAllString(m[1], "Sec")+"="+strings.TrimSpace(m[2]))
			}
		}
		if r.Activates != "" {
			exec, _ := run(ctx, "systemctl", sc("show", r.Activates, "-p", "ExecStart", "--value")...)
			var cmds []string
			for _, m := range execArgv.FindAllStringSubmatch(exec, -1) {
				argv := strings.Fields(m[1])
				cmds = append(cmds, strings.Join(argv, " "))
				if src, ok := scriptOf(argv); ok {
					t.Sources = append(t.Sources, src)
				}
			}
			t.Command = strings.Join(cmds, "\n")
		}
		timers = append(timers, t)
	}
	return timers, nil
}

func usecTime(us uint64) string {
	if us == 0 {
		return ""
	}
	return time.UnixMicro(int64(us)).Format("2006-01-02 15:04")
}

// scriptOf returns the first argv element that is a readable text file (the script itself,
// or the file an interpreter runs, as in `python3 /path/job.py`).
func scriptOf(argv []string) (Source, bool) {
	for _, a := range argv {
		if !filepath.IsAbs(a) {
			continue
		}
		if fi, err := os.Stat(a); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if src := readSource(a); src.Note != "binary" {
			return src, true
		}
	}
	return Source{}, false
}

func readSource(path string) Source {
	src := Source{Path: path}
	f, err := os.Open(path)
	if err != nil {
		src.Note = "unreadable: " + err.Error()
		return src
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, sourceCap+1))
	switch {
	case err != nil:
		src.Note = "unreadable: " + err.Error()
	case bytes.IndexByte(b[:min(len(b), 8192)], 0) >= 0:
		src.Note = "binary"
	case len(b) > sourceCap:
		src.Note = "larger than 64 KiB, not shown"
	default:
		src.Text = string(b)
	}
	return src
}

// CronDir is one place cron reads: a crontab file (/etc/crontab, /etc/cron.d/*) or a run-parts
// directory (/etc/cron.daily etc.) whose scripts run on that period.
type CronDir struct {
	Title, Note string
	Files       []Source
}

// ReadSystemCron reads the system-wide cron configuration and the scripts it runs.
// Root's own crontab (/var/spool/cron/crontabs/root) is not readable without root.
func ReadSystemCron() []CronDir {
	var dirs []CronDir
	if _, err := os.Stat("/etc/crontab"); err == nil {
		dirs = append(dirs, CronDir{Title: "/etc/crontab", Note: "system crontab: minute hour day month weekday user command", Files: []Source{readSource("/etc/crontab")}})
	}
	for _, d := range []struct{ dir, note string }{
		{"/etc/cron.d", "crontab fragments installed by packages, same format as /etc/crontab"},
		{"/etc/cron.hourly", "scripts run every hour (run-parts)"},
		{"/etc/cron.daily", "scripts run every day (run-parts, or anacron when the machine was off)"},
		{"/etc/cron.weekly", "scripts run every week"},
		{"/etc/cron.monthly", "scripts run every month"},
	} {
		entries, err := os.ReadDir(d.dir)
		if err != nil {
			continue
		}
		cd := CronDir{Title: d.dir, Note: d.note}
		for _, e := range entries {
			if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			cd.Files = append(cd.Files, readSource(filepath.Join(d.dir, e.Name())))
		}
		if len(cd.Files) > 0 {
			dirs = append(dirs, cd)
		}
	}
	return dirs
}
