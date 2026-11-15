package host

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	jobTimeout   = 15 * time.Minute
	jobOutputCap = 1 << 20
	jobsKept     = 50
)

// Job is one action (a sequence of commands) running in the background.
// Pages poll it with <meta refresh>, so actions never block a request.
type Job struct {
	ID, Title string
	Started   time.Time

	mu    sync.Mutex
	out   []byte
	ended time.Time
	err   error
	done  bool
}

func (j *Job) Write(p []byte) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if room := jobOutputCap - len(j.out); room > 0 {
		j.out = append(j.out, p[:min(len(p), room)]...)
	}
	return len(p), nil
}

// JobView is a consistent snapshot of a Job for rendering.
type JobView struct {
	ID, Title, Output, Started, Duration, Status string
	Done, Failed                                 bool
}

func (j *Job) View(now time.Time) JobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	v := JobView{ID: j.ID, Title: j.Title, Output: string(j.out), Started: j.Started.Format("2006-01-02 15:04:05"), Done: j.done}
	end := now
	switch {
	case !j.done:
		v.Status = "running"
	case j.err != nil:
		v.Status, v.Failed, end = "failed: "+j.err.Error(), true, j.ended
	default:
		v.Status, end = "done", j.ended
	}
	v.Duration = FmtDuration(end.Sub(j.Started))
	return v
}

type Jobs struct {
	mu    sync.Mutex
	m     map[string]*Job
	order []string // oldest first
}

func NewJobs() *Jobs { return &Jobs{m: map[string]*Job{}} }

// Start runs steps in dir in the background; the newest jobsKept are kept.
func (js *Jobs) Start(title, dir string, steps [][]string) *Job {
	b := make([]byte, 8)
	rand.Read(b)
	j := &Job{ID: hex.EncodeToString(b), Title: title, Started: time.Now()}

	js.mu.Lock()
	js.m[j.ID] = j
	js.order = append(js.order, j.ID)
	for len(js.order) > jobsKept {
		delete(js.m, js.order[0])
		js.order = js.order[1:]
	}
	js.mu.Unlock()

	go j.run(dir, steps)
	return j
}

func (j *Job) run(dir string, steps [][]string) {
	ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	defer cancel()
	var err error
	for _, argv := range steps {
		fmt.Fprintf(j, "$ %s\n", strings.Join(argv, " "))
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Dir = dir
		cmd.Stdout, cmd.Stderr = j, j
		if err = cmd.Run(); err != nil {
			break
		}
	}
	j.mu.Lock()
	j.done, j.err, j.ended = true, err, time.Now()
	j.mu.Unlock()
	slog.Info("job finished", "id", j.ID, "title", j.Title, "err", err)
}

func (js *Jobs) Get(id string) (*Job, bool) {
	js.mu.Lock()
	defer js.mu.Unlock()
	j, ok := js.m[id]
	return j, ok
}

// List returns jobs newest first.
func (js *Jobs) List() []*Job {
	js.mu.Lock()
	defer js.mu.Unlock()
	out := make([]*Job, 0, len(js.order))
	for i := len(js.order) - 1; i >= 0; i-- {
		out = append(out, js.m[js.order[i]])
	}
	return out
}
