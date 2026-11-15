package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// ErrUnsupported marks a feature this OS doesn't have (systemd timers on macOS).
var ErrUnsupported = errors.New("not supported on " + runtime.GOOS)

type System struct {
	Host, Uptime, Load string
	CPUs               int
	MemUsed, MemTotal  string
	MemPct             int
	DiskUsed, DiskSize string
	DiskPct            int
}

const na = "n/a"

// ReadSystem reads uptime, load, memory and disk; fields it cannot read show "n/a".
// The OS-specific readers are in system_linux.go and system_darwin.go.
func ReadSystem() System {
	s := System{CPUs: runtime.NumCPU(), Uptime: na, Load: na, MemUsed: na, MemTotal: na, DiskUsed: na, DiskSize: na}
	s.Host, _ = os.Hostname()
	if d, ok := readUptime(); ok {
		s.Uptime = FmtDuration(d)
	}
	if l, ok := readLoad(); ok {
		s.Load = l
	}
	if total, used, ok := readMemory(); ok {
		s.MemUsed, s.MemTotal = fmtBytes(used), fmtBytes(total)
		s.MemPct = int(used * 100 / total)
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(diskPath, &st); err == nil {
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

// Unit is a service: a systemd unit on Linux, a launchd job on macOS.
type Unit struct {
	Name, Load, Active, Sub, Description string
}

func ReadCrontab(ctx context.Context) (string, error) {
	out, err := run(ctx, "crontab", "-l")
	if err != nil && strings.Contains(err.Error(), "no crontab") {
		return "", nil
	}
	return out, err
}
