// Package claude is everything that knows the shape of an agent harness's own
// configuration directory.
//
// Isolated here because it is the only part of a module that depends on how a
// particular agent lays its files out, and because more than one module needs it:
// harness swaps these paths, and a hooks or skills module would read the same
// ones.
package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/esmarkowski/plasticity-modules/internal/plst"
)

// Dir is the agent's configuration directory: plst's claude_dir setting, else ~/.claude.
func Dir() string {
	if d := plst.LoadConfig().ClaudeDir; d != "" {
		return resolve(d)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".claude"
	}
	return filepath.Join(home, ".claude")
}

// Configured reports whether claude_dir is set, as opposed to Dir falling back to the default.
func Configured() bool { return plst.LoadConfig().ClaudeDir != "" }

// SetDir points plst at an agent directory as typed on a command line: ~ is home,
// and anything else relative is relative to here. It stores and returns the absolute path.
func SetDir(arg string) (string, error) {
	dir, err := filepath.Abs(expandHome(arg))
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(dir); err == nil && !fi.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	return dir, plst.SetClaudeDir(dir)
}

// ResetDir removes the setting, so Dir falls back to the default.
func ResetDir() error { return plst.SetClaudeDir("") }

// EnvOverride is CLAUDE_CONFIG_DIR when it is set to somewhere other than Dir: what an
// agent started from this shell would read instead of what plst is configuring.
func EnvOverride() (string, bool) {
	e := os.Getenv("CLAUDE_CONFIG_DIR")
	if e == "" || filepath.Clean(expandHome(e)) == filepath.Clean(Dir()) {
		return "", false
	}
	return e, true
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// resolve makes a configured path absolute, since a relative one would name a different place from every shell.
func resolve(p string) string {
	p = expandHome(p)
	if !filepath.IsAbs(p) {
		return filepath.Join(plst.Home(), p)
	}
	return p
}

// SettingsPath is the user-scope settings file, where hooks are registered.
func SettingsPath() string { return SettingsIn(Dir()) }

// SettingsIn is the settings file of one agent directory.
func SettingsIn(dir string) string { return filepath.Join(dir, "settings.json") }

// ProjectDir is a repository's own configuration directory.
func ProjectDir(root string) string { return filepath.Join(root, ".claude") }

// ProjectSettingsPath is a repository's settings file.
func ProjectSettingsPath(root string) string {
	return filepath.Join(ProjectDir(root), "settings.json")
}

// Component is one swappable part of a harness.
type Component struct {
	// Name is what the component is called in a harness, and what it is called
	// in the agent's directory. They are the same on purpose: a harness is
	// readable as the thing it becomes.
	Name string
	// Dir distinguishes a directory from a single file, because moving an
	// existing one aside has to know which it is.
	Dir bool
	// ProjectOnly marks a component the agent only ever reads from a project.
	//
	// Rules are the case, and it is not obvious: ~/.claude/rules is never read.
	// Measured on this machine, thirteen rule loads, every one of them from a
	// project's own .claude/rules and none from user scope. Linking one at user
	// scope would appear to work and do nothing at all.
	ProjectOnly bool
}

// Components are the parts of a harness, in the order they are reported.
//
// A closed list on purpose. The agent's configuration directory also holds live
// state — a daemon lock, job directories, a history file, plan files, caches —
// and swapping a harness must never touch any of it.
var Components = []Component{
	{Name: "CLAUDE.md"},
	{Name: "rules", Dir: true, ProjectOnly: true},
	{Name: "agents", Dir: true},
	{Name: "skills", Dir: true},
	{Name: "commands", Dir: true},
	{Name: "hooks", Dir: true},
}

// Component finds one by name.
func Find(name string) (Component, bool) {
	for _, c := range Components {
		if c.Name == name {
			return c, true
		}
	}
	return Component{}, false
}
