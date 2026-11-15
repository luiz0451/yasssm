package host

import (
	"strings"
	"testing"
	"time"
)

func TestLastLines(t *testing.T) {
	in := strings.Join([]string{
		"2026-11-14T23:59:58.5Z one",
		"2026-11-15T00:00:01Z two",
		"Error response from daemon: no such container",
		"2026-11-15T00:00:02.123456789Z three  spaced",
		"2026-11-15T00:00:03Z four",
	}, "\n") + "\n"

	got, err := lastLines(strings.NewReader(in), 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []LogLine{
		{Text: "Error response from daemon: no such container"},
		{Time: time.Date(2026, 11, 15, 0, 0, 2, 123456789, time.UTC), Text: "three  spaced"},
		{Time: time.Date(2026, 11, 15, 0, 0, 3, 0, time.UTC), Text: "four"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if !got[i].Time.Equal(want[i].Time) || got[i].Text != want[i].Text {
			t.Errorf("line %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	got, _ = lastLines(strings.NewReader(in), 10)
	if len(got) != 5 || got[0].Text != "one" || got[4].Text != "four" {
		t.Errorf("fewer lines than n: got %+v", got)
	}
}
