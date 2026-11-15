package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"yasssm/assets"
	"yasssm/internal/host"
	"yasssm/views"
)

const (
	cookieName  = "yasssm_session"
	listTimeout = 15 * time.Second
)

type app struct {
	cfg      config
	host     string
	totp     *totp
	sessions *sessions
	limiter  *limiter
	jobs     *host.Jobs
}

func newApp(cfg config, tp *totp) *app {
	h, _ := os.Hostname()
	return &app{
		cfg:      cfg,
		host:     h,
		totp:     tp,
		sessions: newSessions(cfg.SessionTTL),
		limiter:  &limiter{},
		jobs:     host.NewJobs(),
	}
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(assets.FS)))
	mux.HandleFunc("GET /login", a.loginPage)
	mux.HandleFunc("POST /login", a.login)
	mux.HandleFunc("POST /logout", a.logout)
	mux.HandleFunc("GET /{$}", a.dashboard)
	mux.HandleFunc("GET /logs/{name}", a.logs)
	mux.HandleFunc("GET /confirm", a.confirm)
	mux.HandleFunc("POST /action", a.action)
	mux.HandleFunc("GET /jobs", a.jobList)
	mux.HandleFunc("GET /jobs/{id}", a.jobPage)
	mux.HandleFunc("GET /services", a.services)
	mux.HandleFunc("GET /cron", a.cron)
	return securityHeaders(http.NewCrossOriginProtection().Handler(a.requireAuth(mux)))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// No scripts at all. Inline styles are allowed because shadcn-templ
		// components set some (e.g. the progress bar width).
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'none'; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (a *app) authed(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	return err == nil && a.sessions.valid(c.Value, time.Now())
}

func (a *app) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" || strings.HasPrefix(r.URL.Path, "/assets/") || a.authed(r) {
			next.ServeHTTP(w, r)
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
}

func (a *app) page(r *http.Request, title, nav string) views.Page {
	return views.Page{Title: title, Host: a.host, Nav: nav, Authed: a.authed(r)}
}

func (a *app) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	var buf bytes.Buffer
	if err := c.Render(r.Context(), &buf); err != nil {
		slog.Error("render", "path", r.URL.Path, "err", err)
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (a *app) fail(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, host.ErrNotFound) {
		status = http.StatusNotFound
	}
	a.render(w, r, status, views.ErrorPage(a.page(r, "Error", ""), err.Error()))
}

// --- auth ---

func (a *app) loginPage(w http.ResponseWriter, r *http.Request) {
	if a.authed(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	a.render(w, r, http.StatusOK, views.LoginPage(a.page(r, "Log in", ""), ""))
}

func (a *app) login(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	p := a.page(r, "Log in", "")
	if wait := a.limiter.wait(now); wait > 0 {
		a.render(w, r, http.StatusTooManyRequests, views.LoginPage(p, "Too many attempts. Try again in "+host.FmtDuration(wait)+"."))
		return
	}
	if !a.totp.verify(r.PostFormValue("code"), now) {
		a.limiter.fail(now)
		slog.Warn("login failed", "remote", r.RemoteAddr)
		a.render(w, r, http.StatusUnauthorized, views.LoginPage(p, "Invalid or already used code."))
		return
	}
	a.limiter.reset()
	slog.Info("login", "remote", r.RemoteAddr)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    a.sessions.create(now),
		Path:     "/",
		MaxAge:   int(a.cfg.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   r.TLS != nil || a.cfg.SecureCookie,
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *app) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		a.sessions.delete(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// --- dashboard ---

func confirmURL(kind, name, act string) string {
	return "/confirm?" + url.Values{"kind": {kind}, "name": {name}, "action": {act}}.Encode()
}

func (a *app) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), listTimeout)
	defer cancel()
	d := views.Dashboard{System: host.ReadSystem()}
	if err := a.fillStacks(ctx, &d); err != nil {
		d.Err = err.Error()
	}
	a.render(w, r, http.StatusOK, views.DashboardPage(a.page(r, "Stacks", "stacks"), d))
}

func actionLink(kind, name string, act host.Action) views.Link {
	return views.Link{Action: act.Name, Label: act.Label, URL: confirmURL(kind, name, act.Name), Danger: act.Danger}
}

// containerRow offers only the actions that make sense in the current state.
// A container that exited with code 0 (a one-shot job such as a migration)
// shows as "completed", not as a failure.
func containerRow(c host.Container) views.ContainerRow {
	row := views.ContainerRow{Container: c, State: c.State, Ports: host.FormatPorts(c.Ports), LogsURL: "/logs/" + url.PathEscape(c.Name)}
	switch h := host.Health(c.Status); {
	case c.State == "running" && (h == "unhealthy" || h == "starting"):
		row.State = h
	case c.State == "exited" && strings.HasPrefix(c.Status, "Exited (0)"):
		row.State = "completed"
	}
	acts := []string{"start"}
	if c.State == "running" || c.State == "restarting" || c.State == "paused" {
		acts = []string{"restart", "stop"}
	}
	for _, name := range acts {
		spec, _ := host.LookupAction(host.ContainerActions, name)
		row.Actions = append(row.Actions, actionLink("container", c.Name, spec))
	}
	return row
}

// stateCounts tallies container states in first-seen order. Health is not a
// separate bucket (an unhealthy container still counts as running), but a
// completed one-shot is.
func stateCounts(rows []views.ContainerRow) []host.StateCount {
	var out []host.StateCount
	idx := map[string]int{}
	for _, r := range rows {
		k := r.Container.State
		if r.State == "completed" {
			k = "completed"
		}
		i, ok := idx[k]
		if !ok {
			i = len(out)
			idx[k] = i
			out = append(out, host.StateCount{State: k})
		}
		out[i].N++
	}
	return out
}

// stackActionNames hides actions that would do nothing: Up only when
// something is down, Restart and Stop only when something runs. Completed
// one-shots count as neither.
func stackActionNames(counts []host.StateCount) []string {
	running, other := 0, 0
	for _, c := range counts {
		switch c.State {
		case "running":
			running += c.N
		case "completed":
		default:
			other += c.N
		}
	}
	switch {
	case running == 0:
		return []string{"up", "update"}
	case other > 0:
		return []string{"up", "restart", "update", "stop"}
	}
	return []string{"restart", "update", "stop"}
}

func (a *app) fillStacks(ctx context.Context, d *views.Dashboard) error {
	stacks, err := host.ListStacks(ctx, a.cfg.StacksDir)
	if err != nil {
		return err
	}
	containers, err := host.ListContainers(ctx)
	if err != nil {
		return err
	}
	d.Stacks, d.Total = len(stacks), len(containers)

	byProject := map[string][]views.ContainerRow{}
	for _, c := range containers {
		if c.State == "running" {
			d.Running++
		}
		byProject[c.Project] = append(byProject[c.Project], containerRow(c))
	}

	home, _ := os.UserHomeDir()
	for _, s := range stacks {
		g := views.Group{Title: s.Name, Dir: s.Dir, Rows: byProject[s.Project]}
		if home != "" && strings.HasPrefix(g.Dir, home+"/") {
			g.Dir = "~" + strings.TrimPrefix(g.Dir, home)
		}
		for _, f := range s.Files {
			if rel, err := filepath.Rel(s.Dir, f); err == nil {
				f = rel
			}
			g.Files = append(g.Files, f)
		}
		g.Counts = stateCounts(g.Rows)
		if len(g.Counts) == 0 {
			g.Counts = []host.StateCount{{State: "not created"}}
		}
		for _, name := range stackActionNames(g.Counts) {
			spec, _ := host.LookupAction(host.StackActions, name)
			g.Actions = append(g.Actions, actionLink("stack", s.Name, spec))
		}
		delete(byProject, s.Project)
		d.Groups = append(d.Groups, g)
	}

	var rest []string
	for p := range byProject {
		if p != "" {
			rest = append(rest, p)
		}
	}
	sort.Strings(rest)
	for _, p := range rest {
		rows := byProject[p]
		d.Groups = append(d.Groups, views.Group{Title: p, Note: "Compose project with no compose file under " + a.cfg.StacksDir, Rows: rows, Counts: stateCounts(rows)})
	}
	if rows := byProject[""]; len(rows) > 0 {
		d.Groups = append(d.Groups, views.Group{Title: "Standalone containers", Note: "Not managed by Compose", Rows: rows, Counts: stateCounts(rows)})
	}
	return nil
}

// --- logs ---

func (a *app) logs(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	c, err := host.FindContainer(ctx, r.PathValue("name"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	n, err := strconv.Atoi(r.URL.Query().Get("n"))
	if err != nil {
		n = 200
	}
	n = min(max(n, 10), 5000)
	d := views.Logs{Container: containerRow(c), Output: host.ContainerLogs(ctx, c.Name, n), N: n, Sizes: []int{100, 200, 500, 1000, 5000}}
	a.render(w, r, http.StatusOK, views.LogsPage(a.page(r, "Logs: "+c.Name, "stacks"), d))
}

// --- actions ---

type target struct {
	views.Confirm
	Dir   string
	Steps [][]string
}

// resolve validates kind/name/action against live listings; user input never
// reaches a command line unless it names an existing stack or container.
func (a *app) resolve(ctx context.Context, kind, name, act string) (target, error) {
	t := target{Confirm: views.Confirm{Kind: kind, Name: name, Action: act}}
	switch kind {
	case "container":
		spec, ok := host.LookupAction(host.ContainerActions, act)
		if !ok {
			return t, host.ErrNotFound
		}
		c, err := host.FindContainer(ctx, name)
		if err != nil {
			return t, err
		}
		t.Label, t.Danger, t.Steps = spec.Label, spec.Danger, host.ContainerSteps(c, act)
	case "stack":
		spec, ok := host.LookupAction(host.StackActions, act)
		if !ok {
			return t, host.ErrNotFound
		}
		s, err := host.FindStack(ctx, a.cfg.StacksDir, name)
		if err != nil {
			return t, err
		}
		t.Label, t.Danger, t.Dir, t.Steps = spec.Label, spec.Danger, s.Dir, host.StackSteps(s, act)
	default:
		return t, host.ErrNotFound
	}
	for _, s := range t.Steps {
		t.Commands = append(t.Commands, strings.Join(s, " "))
	}
	return t, nil
}

func (a *app) confirm(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), listTimeout)
	defer cancel()
	q := r.URL.Query()
	t, err := a.resolve(ctx, q.Get("kind"), q.Get("name"), q.Get("action"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, http.StatusOK, views.ConfirmPage(a.page(r, t.Label+" "+t.Name, "stacks"), t.Confirm))
}

func (a *app) action(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), listTimeout)
	defer cancel()
	t, err := a.resolve(ctx, r.PostFormValue("kind"), r.PostFormValue("name"), r.PostFormValue("action"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	j := a.jobs.Start(t.Label+" "+t.Kind+" "+t.Name, t.Dir, t.Steps)
	slog.Info("action", "job", j.ID, "kind", t.Kind, "name", t.Name, "action", t.Action, "remote", r.RemoteAddr)
	http.Redirect(w, r, "/jobs/"+j.ID, http.StatusSeeOther)
}

func (a *app) jobPage(w http.ResponseWriter, r *http.Request) {
	j, ok := a.jobs.Get(r.PathValue("id"))
	if !ok {
		a.fail(w, r, host.ErrNotFound)
		return
	}
	v := j.View(time.Now())
	p := a.page(r, v.Title, "jobs")
	if !v.Done {
		p.Refresh = 2
	}
	a.render(w, r, http.StatusOK, views.JobPage(p, v))
}

func (a *app) jobList(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	var vs []host.JobView
	for _, j := range a.jobs.List() {
		vs = append(vs, j.View(now))
	}
	a.render(w, r, http.StatusOK, views.JobsPage(a.page(r, "Jobs", "jobs"), vs))
}

// --- services & cron ---

func (a *app) services(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), listTimeout)
	defer cancel()
	var lists []views.UnitList
	for _, user := range []bool{false, true} {
		l := views.UnitList{Title: "System services", User: user}
		if user {
			l.Title = "User services"
		}
		units, err := host.ListUnits(ctx, user)
		if err != nil {
			l.Err = err.Error()
		}
		sort.SliceStable(units, func(i, j int) bool {
			return units[i].Active == "failed" && units[j].Active != "failed"
		})
		for _, u := range units {
			if u.Active == "failed" {
				l.Failed++
			}
		}
		l.Units = units
		lists = append(lists, l)
	}
	a.render(w, r, http.StatusOK, views.ServicesPage(a.page(r, "Services", "services"), lists))
}

func (a *app) cron(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), listTimeout)
	defer cancel()
	out, err := host.ReadCrontab(ctx)
	d := views.Cron{Crontab: out}
	if err != nil {
		d.Err = err.Error()
	}
	d.System = host.ReadSystemCron()
	for _, user := range []bool{true, false} {
		g := views.TimerGroup{Title: "System timers", User: user}
		if user {
			g.Title = "User timers"
		}
		if g.Timers, err = host.ListTimers(ctx, user); err != nil {
			g.Err = err.Error()
		}
		d.Timers = append(d.Timers, g)
	}
	a.render(w, r, http.StatusOK, views.CronPage(a.page(r, "Cron", "cron"), d))
}
