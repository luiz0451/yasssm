// Package host reads and changes the state of the machine yasssm runs on:
// Docker containers and Compose stacks, systemd units, cron and /proc.
package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var ErrNotFound = errors.New("not found")

type Container struct {
	Name, Image, State, Status, Ports, Project, Service string
}

type Stack struct {
	Name    string // directory name under the stacks dir
	Dir     string
	Files   []string // compose files, in -f order
	Project string
}

// run runs a command and returns stdout; stderr becomes the error text.
func run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return string(out), fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
	}
	return string(out), nil
}

const psFormat = "{{.Names}}\t{{.Image}}\t{{.State}}\t{{.Status}}\t{{.Ports}}\t" +
	`{{.Label "com.docker.compose.project"}}` + "\t" + `{{.Label "com.docker.compose.service"}}`

func ListContainers(ctx context.Context) ([]Container, error) {
	out, err := run(ctx, "docker", "ps", "-a", "--format", psFormat)
	if err != nil {
		return nil, err
	}
	var cs []Container
	for line := range strings.Lines(out) {
		f := strings.Split(strings.TrimRight(line, "\n"), "\t")
		if len(f) != 7 {
			continue
		}
		cs = append(cs, Container{f[0], f[1], f[2], f[3], f[4], f[5], f[6]})
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].Name < cs[j].Name })
	return cs, nil
}

func FindContainer(ctx context.Context, name string) (Container, error) {
	cs, err := ListContainers(ctx)
	if err != nil {
		return Container{}, err
	}
	for _, c := range cs {
		if c.Name == name {
			return c, nil
		}
	}
	return Container{}, ErrNotFound
}

// ContainerLogs returns the last n lines of a container's stdout and stderr.
func ContainerLogs(ctx context.Context, name string, n int) string {
	out, err := exec.CommandContext(ctx, "docker", "logs", "--tail", fmt.Sprint(n), "--timestamps", name).CombinedOutput()
	if err != nil {
		out = append(out, []byte("\n[yasssm] "+err.Error())...)
	}
	return string(out)
}

var composeFiles = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// ListStacks lists the directories one level under root that hold a Compose
// project. A project Compose already knows (`docker compose ls`) keeps the
// exact config files it was started with, e.g. a base file plus overlays;
// otherwise the directory needs one of the standard compose file names.
func ListStacks(ctx context.Context, root string) ([]Stack, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	type lsItem struct{ Name, Status, ConfigFiles string }
	var items []lsItem
	out, err := run(ctx, "docker", "compose", "ls", "-a", "--format", "json")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		return nil, fmt.Errorf("docker compose ls: %w", err)
	}
	byDir := map[string]lsItem{}
	for _, it := range items {
		if first, _, _ := strings.Cut(it.ConfigFiles, ","); first != "" {
			byDir[filepath.Dir(first)] = it
		}
	}

	var stacks []Stack
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if it, ok := byDir[dir]; ok {
			stacks = append(stacks, Stack{Name: e.Name(), Dir: dir, Files: strings.Split(it.ConfigFiles, ","), Project: it.Name})
			continue
		}
		for _, n := range composeFiles {
			file := filepath.Join(dir, n)
			if _, err := os.Stat(file); err == nil {
				stacks = append(stacks, Stack{Name: e.Name(), Dir: dir, Files: []string{file}, Project: defaultProject(e.Name())})
				break
			}
		}
	}
	return stacks, nil
}

func FindStack(ctx context.Context, root, name string) (Stack, error) {
	stacks, err := ListStacks(ctx, root)
	if err != nil {
		return Stack{}, err
	}
	for _, s := range stacks {
		if s.Name == name {
			return s, nil
		}
	}
	return Stack{}, ErrNotFound
}

// defaultProject mirrors Compose's project name derived from a directory.
func defaultProject(dir string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(dir) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type Action struct {
	Name, Label string
	Danger      bool
}

var ContainerActions = []Action{
	{"start", "Start", false},
	{"restart", "Restart", false},
	{"stop", "Stop", true},
}

var StackActions = []Action{
	{"up", "Up", false},
	{"restart", "Restart", false},
	{"update", "Pull & recreate", false},
	{"stop", "Stop", true},
}

func LookupAction(list []Action, name string) (Action, bool) {
	for _, a := range list {
		if a.Name == name {
			return a, true
		}
	}
	return Action{}, false
}

func ContainerSteps(c Container, act string) [][]string {
	return [][]string{{"docker", act, c.Name}}
}

func StackSteps(s Stack, act string) [][]string {
	compose := func(extra ...string) []string {
		argv := []string{"docker", "compose"}
		for _, f := range s.Files {
			argv = append(argv, "-f", f)
		}
		return append(argv, extra...)
	}
	switch act {
	case "up":
		return [][]string{compose("up", "-d")}
	case "restart":
		return [][]string{compose("restart")}
	case "stop":
		return [][]string{compose("stop")}
	case "update":
		return [][]string{compose("pull", "--ignore-buildable"), compose("up", "-d")}
	}
	return nil
}
