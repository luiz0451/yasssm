package host

import "strings"

// FormatPorts turns `docker ps` port bindings into short host→container
// pairs: wildcard addresses are dropped, IPv4/IPv6 duplicates merged, /tcp
// implied. Exposed but unpublished ports are left out.
//
//	"0.0.0.0:8000->80/tcp, [::]:8000->80/tcp, 127.0.0.1:53->53/udp, 9000/tcp"
//	→ ["8000→80", "127.0.0.1:53→53/udp"]
func FormatPorts(s string) []string {
	var out []string
	seen := map[string]bool{}
	for p := range strings.SplitSeq(s, ", ") {
		hostPart, ctr, ok := strings.Cut(strings.TrimSpace(p), "->")
		if !ok {
			continue
		}
		for _, wild := range []string{"0.0.0.0:", "[::]:", ":::"} {
			hostPart = strings.TrimPrefix(hostPart, wild)
		}
		ctr = strings.TrimSuffix(ctr, "/tcp")
		pair := hostPart + "→" + ctr
		if !seen[pair] {
			seen[pair] = true
			out = append(out, pair)
		}
	}
	return out
}

type StateCount struct {
	State string
	N     int
}

// Health reads the health suffix of a `docker ps` status ("Up 2h (unhealthy)").
func Health(status string) string {
	switch {
	case strings.Contains(status, "(unhealthy)"):
		return "unhealthy"
	case strings.Contains(status, "(health: starting)"):
		return "starting"
	case strings.Contains(status, "(healthy)"):
		return "healthy"
	}
	return ""
}
