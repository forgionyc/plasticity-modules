package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/esmarkowski/plasticity-modules/internal/claude"
)

// Report is what applying a harness did, for the caller to print.
type Report struct {
	Harness  string
	Scope    Scope
	Linked   []string
	Parked   []string
	Skipped  []string
	Hooks    []string
	Replaced string
}

// Use puts a harness in place.
//
// Symlinks, not copies. It was worth checking rather than assuming: a scratch
// project with a symlinked CLAUDE.md, a symlinked agents directory, and an
// agents directory holding a symlinked entry were each probed for what the agent
// reported loading, and all three loaded — directory links and entry links alike.
// So a harness stays the one copy of its own files, an edit made while it is
// active is an edit to the harness, and swapping is instant.
func Use(name string, scope Scope, say func(string)) (Report, error) {
	h, err := Find(name)
	if err != nil {
		return Report{}, err
	}
	scope = scope.Resolve()
	target, settings, err := scopePaths(scope)
	if err != nil {
		return Report{}, err
	}

	state := LoadState()
	rep := Report{Harness: h.Name, Scope: scope}

	// Whatever is already here comes out first. Applying over the top would leave
	// the previous harness's hooks registered and its parked files unclaimed.
	if prev, ok := state.Active(scope); ok {
		if prev.Harness == h.Name && prev.Dir == h.Dir {
			say("re-linking " + h.Name)
		} else {
			rep.Replaced = prev.Harness
		}
		if err := revert(&state, scope, prev); err != nil {
			return Report{}, err
		}
	}

	stamp := time.Now().UTC().Format("20060102-150405")
	applied := Applied{Harness: h.Name, Dir: h.Dir, At: time.Now(), Settings: settings}
	_, isProject := scope.IsProject()

	merge := h.Manifest.Mode() == Merge

	for _, c := range Components(h.Dir) {
		if c.ProjectOnly && !isProject {
			// Saying so rather than linking it: the link would be created, look
			// correct, and never be read.
			rep.Skipped = append(rep.Skipped, c.Name)
			continue
		}
		path := filepath.Join(target, c.Name)
		src := filepath.Join(h.Dir, c.Name)

		// A file component is the whole of itself either way — there are no
		// entries in a CLAUDE.md to merge.
		if !c.Dir || !merge {
			link := Link{Path: path, Target: src}
			parked, err := clear(path, stamp, scope)
			if err != nil {
				return Report{}, err
			}
			if parked != "" {
				link.Parked = parked
				rep.Parked = append(rep.Parked, c.Name)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return Report{}, err
			}
			if err := os.Symlink(src, path); err != nil {
				return Report{}, fmt.Errorf("link %s: %w", c.Name, err)
			}
			applied.Links = append(applied.Links, link)
			rep.Linked = append(rep.Linked, c.Name)
			continue
		}

		// Merging: the directory itself stays real and each entry is linked into
		// it, so a repository's own committed agents and skills are still there
		// and only a same-named entry is ever displaced.
		entries, err := os.ReadDir(src)
		if err != nil {
			return Report{}, err
		}
		container, created, err := realDir(path, stamp, scope)
		if err != nil {
			return Report{}, err
		}
		if created {
			applied.Dirs = append(applied.Dirs, path)
		}
		if container.Parked != "" {
			rep.Parked = append(rep.Parked, c.Name)
			applied.Links = append(applied.Links, container)
		}

		linked := 0
		for _, e := range entries {
			// A dotted entry is the harness's own bookkeeping — the .gitkeep that
			// carries an empty directory through git, a .DS_Store — and not
			// something the agent reads. Linking it in would be noise in someone
			// else's repository.
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			name := filepath.Join(c.Name, e.Name())
			entryPath := filepath.Join(path, e.Name())
			link := Link{Path: entryPath, Target: filepath.Join(src, e.Name())}
			parked, err := clear(entryPath, stamp, scope)
			if err != nil {
				return Report{}, err
			}
			if parked != "" {
				link.Parked = parked
				rep.Parked = append(rep.Parked, name)
			}
			if err := os.Symlink(link.Target, entryPath); err != nil {
				return Report{}, fmt.Errorf("link %s: %w", name, err)
			}
			applied.Links = append(applied.Links, link)
			linked++
		}
		rep.Linked = append(rep.Linked, fmt.Sprintf("%s (%d)", c.Name, linked))
	}

	cmds, err := registerHooks(settings, h)
	if err != nil {
		return Report{}, err
	}
	applied.Commands = cmds
	rep.Hooks = cmds

	state.Scopes[scope] = applied
	if err := state.Save(); err != nil {
		return Report{}, err
	}
	return rep, nil
}

// Off takes a harness out of a scope and puts back whatever it displaced.
func Off(scope Scope, say func(string)) (Report, error) {
	scope = scope.Resolve()
	state := LoadState()
	prev, ok := state.Active(scope)
	if !ok {
		return Report{}, fmt.Errorf("no harness is applied at %s%s", scope.Label(), state.elsewhere(scope))
	}
	say("removing " + prev.Harness)
	if err := revert(&state, scope, prev); err != nil {
		return Report{}, err
	}
	if err := state.Save(); err != nil {
		return Report{}, err
	}
	rep := Report{Harness: prev.Harness, Scope: scope}
	// Components for what was taken out, since a merged component is many links
	// and naming each one says nothing. Full paths for what was restored, because
	// there the individual file is the point.
	seen := map[string]bool{}
	for _, l := range prev.Links {
		if c := component(prev, l.Path); l.Target != "" && !seen[c] {
			seen[c] = true
			rep.Linked = append(rep.Linked, c)
		}
		if l.Parked != "" {
			rep.Parked = append(rep.Parked, relative(prev, l.Path))
		}
	}
	return rep, nil
}

// revert undoes one Applied: the links it made, the files it moved, the hooks it
// registered. Best effort throughout — a half-reverted scope is worse than a
// reverted one with a complaint, so it keeps going and reports at the end.
func revert(state *State, scope Scope, a Applied) error {
	var failed []string

	// One: the links. Only a link this program made is removed — if the path is
	// now a real directory, someone replaced it deliberately and it is not plst's
	// to delete.
	for _, l := range a.Links {
		if l.Target == "" {
			continue
		}
		if fi, err := os.Lstat(l.Path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(l.Path); err != nil {
				failed = append(failed, l.Path)
			}
		}
	}

	// Two: the directories created to hold those links, deepest first. os.Remove
	// refuses a directory that is not empty, which is exactly the wanted rule: a
	// directory someone has since put their own file in stays.
	dirs := append([]string(nil), a.Dirs...)
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		_ = os.Remove(d)
	}

	// Three: what was moved aside, now that the paths it belongs at are free.
	for _, l := range a.Links {
		if l.Parked == "" {
			continue
		}
		if _, err := os.Lstat(l.Path); err == nil {
			// Something is in the way; leaving the parked copy where it is beats
			// overwriting whatever that is.
			failed = append(failed, l.Parked)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(l.Path), 0o755); err != nil {
			failed = append(failed, l.Parked)
			continue
		}
		if err := os.Rename(l.Parked, l.Path); err != nil {
			failed = append(failed, l.Parked)
		}
	}
	if err := unregisterHooks(a.Settings, a.Commands); err != nil {
		return err
	}
	delete(state.Scopes, scope)
	if len(failed) > 0 {
		return fmt.Errorf("could not restore: %s", strings.Join(failed, ", "))
	}
	return nil
}

// clear makes a path available, moving anything real out of the way.
//
// A symlink is removed outright: the only symlinks at these paths are ones this
// program made, and a broken one from a harness that was deleted by hand should
// not stop a new one going in. Anything else is moved, never deleted.
func clear(path, stamp string, scope Scope) (parked string, err error) {
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return "", os.Remove(path)
	}
	dir, err := parkDir(stamp + "-" + scopeSlug(scope))
	if err != nil {
		return "", err
	}
	dest := filepath.Join(dir, filepath.Base(path))
	if err := os.Rename(path, dest); err != nil {
		return "", fmt.Errorf("move %s aside: %w", path, err)
	}
	return dest, nil
}

// realDir prepares a genuine directory at path for entry links to go into.
//
// Returns the Link to record when something had to be moved aside, and whether
// the directory itself was created — reverting needs to know both, and they are
// not the same question: a directory that was already there stays.
func realDir(path, stamp string, scope Scope) (link Link, created bool, err error) {
	fi, lerr := os.Lstat(path)
	switch {
	case os.IsNotExist(lerr):
		return Link{Path: path}, true, os.MkdirAll(path, 0o755)
	case lerr != nil:
		return Link{}, false, lerr
	case fi.Mode()&os.ModeSymlink != 0:
		// A link left by a harness that replaced this component rather than
		// merging into it. Taking it out and making a real directory is what
		// switching to merge means.
		if err := os.Remove(path); err != nil {
			return Link{}, false, err
		}
		return Link{Path: path}, true, os.MkdirAll(path, 0o755)
	case fi.IsDir():
		return Link{Path: path}, false, nil
	default:
		// A file where a component directory belongs. Moved aside like anything
		// else, never deleted.
		parked, err := clear(path, stamp, scope)
		if err != nil {
			return Link{}, false, err
		}
		return Link{Path: path, Parked: parked}, true, os.MkdirAll(path, 0o755)
	}
}

// relative names a linked path the way a person reading the output thinks of it:
// relative to the directory the harness was applied to.
func relative(a Applied, path string) string {
	if a.Settings == "" {
		return filepath.Base(path)
	}
	rel, err := filepath.Rel(filepath.Dir(a.Settings), path)
	if err != nil {
		return filepath.Base(path)
	}
	return rel
}

// component is which part of a harness a path belongs to.
func component(a Applied, path string) string {
	return strings.SplitN(relative(a, path), string(os.PathSeparator), 2)[0]
}

// scopeSlug makes a scope usable as a directory name.
func scopeSlug(scope Scope) string {
	if root, ok := scope.IsProject(); ok {
		return "project" + strings.ReplaceAll(root, string(os.PathSeparator), "-")
	}
	return "user"
}

// scopePaths is where a scope's components and settings live.
func scopePaths(scope Scope) (target, settings string, err error) {
	if root, ok := scope.IsProject(); ok {
		abs, err := filepath.Abs(root)
		if err != nil {
			return "", "", err
		}
		return claude.ProjectDir(abs), claude.ProjectSettingsPath(abs), nil
	}
	dir, _ := scope.Resolve().IsUser()
	return dir, claude.SettingsIn(dir), nil
}
