package host

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type System struct {
	Host, Uptime, Load string
	CPUs               int
	MemUsed, MemTotal  string
	MemPct             int
	DiskUsed, DiskSize string
	DiskPct            int
}

const na = "n/a"

// ReadSystem reads Linux /proc; fields it cannot read show "n/a".
func ReadSystem() System {
	s := System{CPUs: runtime.NumCPU(), Uptime: na, Load: na, MemUsed: na, MemTotal: na, DiskUsed: na, DiskSize: na}
	s.Host, _ = os.Hostname()

	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			if secs, err := strconv.ParseFloat(f[0], 64); err == nil {
				s.Uptime = FmtDuration(time.Duration(secs) * time.Second)
			}
		}
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		if f := strings.Fields(string(b)); len(f) >= 3 {
			s.Load = strings.Join(f[:3], " ")
		}
	}
	if total, avail, ok := meminfo(); ok {
		used := total - avail
		s.MemUsed, s.MemTotal = fmtBytes(used), fmtBytes(total)
		s.MemPct = int(used * 100 / total)
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err == nil {
		bs := uint64(st.Bsize)
		size := st.Blocks * bs
		used := size - st.Bfree*bs
		avail := st.Bavail * bs
		s.DiskUsed, s.DiskSize = fmtBytes(used), fmtBytes(size)
		if used+avail > 0 {
			s.DiskPct = int(used * 100 / (used + avail))
		}
	}
	return s
}

func meminfo() (total, avail uint64, ok bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
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
	return total, avail, total > 0
}

func fmtBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func FmtDuration(d time.Duration) string {
	d = d.Round(time.Second)
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	m := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, h)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %ds", m, int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

type Unit struct {
	Name, Load, Active, Sub, Description string
}

// ListUnits lists loaded service units (running, exited or failed).
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

func ReadCrontab(ctx context.Context) (string, error) {
	out, err := run(ctx, "crontab", "-l")
	if err != nil && strings.Contains(err.Error(), "no crontab") {
		return "", nil
	}
	return out, err
}
