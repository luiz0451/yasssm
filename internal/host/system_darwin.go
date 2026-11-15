//go:build darwin

package host

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The data volume: "/" is the read-only system volume on macOS 10.15+.
const diskPath = "/System/Volumes/Data"

var (
	bootSec  = regexp.MustCompile(`sec = (\d+)`)
	pageSize = regexp.MustCompile(`page size of (\d+) bytes`)
)

func sysctl(name string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := run(ctx, "sysctl", "-n", name)
	return strings.TrimSpace(out), err == nil
}

func readUptime() (time.Duration, bool) {
	out, ok := sysctl("kern.boottime") // { sec = 1794763670, usec = 88195 } Sun Nov 15 …
	m := bootSec.FindStringSubmatch(out)
	if !ok || m == nil {
		return 0, false
	}
	sec, err := strconv.ParseInt(m[1], 10, 64)
	return time.Since(time.Unix(sec, 0)), err == nil
}

func readLoad() (string, bool) {
	out, ok := sysctl("vm.loadavg") // { 0.38 0.21 0.21 }
	f := strings.Fields(strings.Trim(out, "{} "))
	if !ok || len(f) < 3 {
		return "", false
	}
	return strings.Join(f[:3], " "), true
}

// readMemory returns total and used bytes, used counted like Activity Monitor's "Memory Used":
// active + wired + compressed pages.
func readMemory() (total, used uint64, ok bool) {
	out, ok := sysctl("hw.memsize")
	if !ok {
		return 0, 0, false
	}
	total, err := strconv.ParseUint(out, 10, 64)
	if err != nil || total == 0 {
		return 0, 0, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	vm, err := run(ctx, "vm_stat")
	if err != nil {
		return 0, 0, false
	}
	var page uint64 = 4096
	if m := pageSize.FindStringSubmatch(vm); m != nil {
		page, _ = strconv.ParseUint(m[1], 10, 64)
	}
	var pages uint64
	for line := range strings.Lines(vm) {
		k, v, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch strings.TrimSpace(k) {
		case "Pages active", "Pages wired down", "Pages occupied by compressor":
			n, _ := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(v), "."), 10, 64)
			pages += n
		}
	}
	return total, min(pages*page, total), true
}

// ListUnits lists the user's launchd agents (macOS has no systemd), leaving out Apple's own
// jobs and the per-launch entries of GUI apps ("application.…"). System daemons need root
// to list: ErrUnsupported.
func ListUnits(ctx context.Context, user bool) ([]Unit, error) {
	if !user {
		return nil, ErrUnsupported
	}
	out, err := run(ctx, "launchctl", "list")
	if err != nil {
		return nil, err
	}
	var units []Unit
	for line := range strings.Lines(out) {
		f := strings.Fields(line) // PID  Status  Label
		if len(f) != 3 || f[2] == "Label" || strings.HasPrefix(f[2], "com.apple.") || strings.HasPrefix(f[2], "application.") {
			continue
		}
		u := Unit{Name: f[2], Load: "loaded", Active: "inactive", Sub: "exit " + f[1]}
		switch {
		case f[0] != "-":
			u.Active, u.Sub = "active", "running (pid "+f[0]+")"
		case f[1] != "0":
			u.Active = "failed"
		}
		units = append(units, u)
	}
	return units, nil
}
