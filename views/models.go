// Package views renders every page server-side with templ and shadcn-templ
// components. Pages carry no JavaScript; forms post and redirect.
package views

import "yasssm/internal/host"

// Page is the per-request layout data.
type Page struct {
	Title, Host string
	Nav         string // active nav item: stacks, services, cron, jobs
	Authed      bool
	Refresh     int // seconds; > 0 adds <meta http-equiv="refresh">
}

// Link is an action button. Action picks the icon: logs, up, start,
// restart, update, stop.
type Link struct {
	Action, Label, URL string
	Danger             bool
}

type ContainerRow struct {
	host.Container
	State   string // State, or the health problem of a running container
	Ports   []string
	LogsURL string
	Actions []Link
}

type Group struct {
	Title   string
	Dir     string   // empty for groups without a compose file
	Files   []string // compose file names, relative to Dir
	Note    string
	Counts  []host.StateCount
	Actions []Link
	Rows    []ContainerRow
}

type Dashboard struct {
	System         host.System
	Groups         []Group
	Stacks         int
	Running, Total int
	Err            string
}

type Logs struct {
	Container ContainerRow
	Output    string
	N         int
	Sizes     []int
}

type Confirm struct {
	Kind, Name, Action, Label string
	Danger                    bool
	Commands                  []string
}

type UnitList struct {
	Title  string
	User   bool
	Units  []host.Unit
	Failed int
	Err    string
}

type Cron struct {
	Crontab, Err string
	System       []host.CronDir
	Timers       []TimerGroup // user timers first
}

type TimerGroup struct {
	Title  string
	User   bool
	Timers []host.Timer
	Err    string
}
