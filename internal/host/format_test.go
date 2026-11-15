package host

import (
	"slices"
	"testing"
)

func TestFormatPorts(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"0.0.0.0:5173->5173/tcp, [::]:5173->5173/tcp, 0.0.0.0:8000->80/tcp, [::]:8000->80/tcp", []string{"5173→5173", "8000→80"}},
		{"127.0.0.1:8090->80/tcp", []string{"127.0.0.1:8090→80"}},
		{"0.0.0.0:53->53/udp, :::53->53/udp, 0.0.0.0:53->53/tcp", []string{"53→53/udp", "53→53"}},
		{"80/tcp, 0.0.0.0:7000-7002->7000-7002/tcp", []string{"7000-7002→7000-7002"}},
	}
	for _, c := range cases {
		if got := FormatPorts(c.in); !slices.Equal(got, c.want) {
			t.Errorf("FormatPorts(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
