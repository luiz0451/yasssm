//go:build linux

package host

import (
	"bufio"
	"context"
	"os"
	"strconv"
	"strings"
	"time"
)

const diskPath = "/"

func readUptime() (time.Duration, bool) {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, false
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0, false
	}
	secs, err := strconv.ParseFloat(f[0], 64)
	return time.Duration(secs) * time.Second, err == nil
}

func readLoad() (string, bool) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return "", false
	}
	f := strings.Fields(string(b))
	if len(f) < 3 {
		return "", false
	}
	return strings.Join(f[:3], " "), true
}

// readMemory returns total and used bytes, used being total minus MemAvailable.
func readMemory() (total, used uint64, ok bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	var avail uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, found := strings.Cut(sc.Text(), ":")
		if !found {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(v), " kB"), 10, 64)
		if err != nil {
			continue
		}
		switch k {
		case "MemTotal":
			total = n * 1024
		case "MemAvailable":
			avail = n * 1024
		}
	}
	return total, total - avail, total > 0
}

// ListUnits lists loaded systemd service units (running, exited or failed).
func ListUnits(ctx context.Context, user bool) ([]Unit, error) {
	args := []string{"list-units", "--type=service", "--no-pager", "--plain", "--no-legend"}
	if user {
		args = append([]string{"--user"}, args...)
	}
	out, err := run(ctx, "systemctl", args...)
	if err != nil {
		return nil, err
	}
	var units []Unit
	for line := range strings.Lines(out) {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		units = append(units, Unit{f[0], f[1], f[2], f[3], strings.Join(f[4:], " ")})
	}
	return units, nil
}
