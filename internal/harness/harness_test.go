package harness

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/esmarkowski/plasticity-modules/internal/claude"
	"github.com/esmarkowski/plasticity-modules/internal/plst"
)

// scratch points plst's state and the agent's configuration directory at
// temporary directories, so a test can apply harnesses without touching either
// real one.
func scratch(t *testing.T) (home, agent string) {
	t.Helper()
	home, agent = filepath.Join(t.TempDir(), "plst"), filepath.Join(t.TempDir(), "agent")
	cfg := filepath.Join(home, "config.json")
	t.Setenv(plst.EnvHome, home)
	t.Setenv(plst.EnvConfig, cfg)
	if err := os.MkdirAll(agent, 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(plst.Config{ClaudeDir: agent})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, cfg, string(b))
	return home, agent
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// build a harness on disk with the components named.
func makeHarness(t *testing.T, name string, hooks string, components ...string) Harness {
	t.Helper()
	root, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, name)
	for _, c := range components {
		if c == "CLAUDE.md" {
			writeFile(t, filepath.Join(dir, c), "# "+name+"\n")
			continue
		}
		writeFile(t, filepath.Join(dir, c, "item.md"), "item\n")
	}
	manifest := `{"name":"` + name + `"`
	if hooks != "" {
		manifest += `,"hooks":` + hooks
	}
	manifest += "}"
	writeFile(t, filepath.Join(dir, ManifestFile), manifest)
	h, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// A harness is a repository that may be cloned anywhere, so a hook command that
// names a path is a hook command that works on one machine.
func TestExpandSubstitutesTheRoot(t *testing.T) {
	m := Manifest{Hooks: map[string][]claude.HookGroup{
		"PreToolUse": {{Matcher: "Bash", Hooks: []json.RawMessage{
			json.RawMessage(`{"type":"command","command":"` + RootVar + `/hooks/guard.sh","timeout":5}`),
		}}},
	}}
	got := m.Expand("/opt/h")
	cmds := claude.Commands(got["PreToolUse"])
	if len(cmds) != 1 || cmds[0] != "/opt/h/hooks/guard.sh" {
		t.Fatalf("commands = %v", cmds)
	}
	// Fields this program has never heard of survive the round trip.
	if !strings.Contains(string(got["PreToolUse"][0].Hooks[0]), `"timeout":5`) {
		t.Errorf("an unknown field was dropped: %s", got["PreToolUse"][0].Hooks[0])
	}
	// The manifest itself is untouched, so a harness used once does not come back
	// with a machine's paths baked into it.
	if !strings.Contains(string(m.Hooks["PreToolUse"][0].Hooks[0]), RootVar) {
		t.Error("Expand mutated the manifest it was given")
	}
}

// What was in the agent's directory before plst arrived is not plst's to throw
// away. A file component is the whole of itself, so this one is displaced and
// comes back.
func TestUseParksWhatItDisplacesAndOffPutsItBack(t *testing.T) {
	_, agent := scratch(t)
	writeFile(t, filepath.Join(agent, "CLAUDE.md"), "ORIGINAL\n")
	makeHarness(t, "rails", "", "CLAUDE.md", "agents")

	rep, err := Use("rails", User, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Parked) != 1 || rep.Parked[0] != "CLAUDE.md" {
		t.Errorf("parked %v, want the pre-existing CLAUDE.md", rep.Parked)
	}
	// The link is live and resolves to the harness.
	if b, _ := os.ReadFile(filepath.Join(agent, "CLAUDE.md")); string(b) != "# rails\n" {
		t.Errorf("CLAUDE.md reads %q", b)
	}

	if _, err := Off(User, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(agent, "CLAUDE.md")); string(b) != "ORIGINAL\n" {
		t.Errorf("after off, CLAUDE.md reads %q, want the original back", b)
	}
	if _, ok := LoadState().Active(User); ok {
		t.Error("state still reports a harness applied")
	}
}

// The point of merging. A repository commits its own agents and skills, and a
// harness has to be able to contribute beside them rather than over them —
// otherwise using one shows those files as deleted in git status.
func TestMergeLeavesAProjectsOwnEntriesInPlace(t *testing.T) {
	_, agent := scratch(t)
	writeFile(t, filepath.Join(agent, "agents", "mine.md"), "MY AGENT\n")
	makeHarness(t, "rails", "", "agents")

	rep, err := Use("rails", User, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Parked) != 0 {
		t.Errorf("parked %v, want nothing moved aside", rep.Parked)
	}
	if len(rep.Linked) != 1 || rep.Linked[0] != "agents (1)" {
		t.Errorf("linked = %v, want the entry count", rep.Linked)
	}
	// The directory is still a directory, holding both.
	if fi, err := os.Lstat(filepath.Join(agent, "agents")); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Error("agents was replaced by a symlink")
	}
	if b, _ := os.ReadFile(filepath.Join(agent, "agents", "mine.md")); string(b) != "MY AGENT\n" {
		t.Errorf("the project's own agent reads %q while the harness is applied", b)
	}
	if b, _ := os.ReadFile(filepath.Join(agent, "agents", "item.md")); string(b) != "item\n" {
		t.Errorf("the harness's agent reads %q", b)
	}

	if _, err := Off(User, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(agent, "agents", "mine.md")); string(b) != "MY AGENT\n" {
		t.Errorf("after off, the project's own agent is %q", b)
	}
	if _, err := os.Lstat(filepath.Join(agent, "agents", "item.md")); err == nil {
		t.Error("the harness's entry was left behind")
	}
}

// Merging narrows the collision to one name, it does not abolish it. A
// same-named entry is still moved aside, and still comes back.
func TestMergeParksOnlyASameNamedEntry(t *testing.T) {
	_, agent := scratch(t)
	writeFile(t, filepath.Join(agent, "agents", "item.md"), "MINE\n")
	writeFile(t, filepath.Join(agent, "agents", "other.md"), "UNTOUCHED\n")
	makeHarness(t, "rails", "", "agents")

	rep, err := Use("rails", User, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Parked) != 1 || rep.Parked[0] != filepath.Join("agents", "item.md") {
		t.Errorf("parked %v, want only the colliding entry", rep.Parked)
	}
	if b, _ := os.ReadFile(filepath.Join(agent, "agents", "item.md")); string(b) != "item\n" {
		t.Errorf("the harness entry did not win: %q", b)
	}

	if _, err := Off(User, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(agent, "agents", "item.md")); string(b) != "MINE\n" {
		t.Errorf("after off, the parked entry is %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(agent, "agents", "other.md")); string(b) != "UNTOUCHED\n" {
		t.Errorf("an uninvolved entry changed: %q", b)
	}
}

// A .gitkeep is how an empty directory survives git, and a .DS_Store is noise.
// Neither is something the agent reads, so neither is linked into someone's
// repository.
func TestMergeSkipsDottedEntries(t *testing.T) {
	_, agent := scratch(t)
	h := makeHarness(t, "rails", "", "agents")
	writeFile(t, filepath.Join(h.Dir, "agents", ".gitkeep"), "")

	rep, err := Use("rails", User, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Linked) != 1 || rep.Linked[0] != "agents (1)" {
		t.Errorf("linked = %v, want only the real entry counted", rep.Linked)
	}
	if _, err := os.Lstat(filepath.Join(agent, "agents", ".gitkeep")); err == nil {
		t.Error(".gitkeep was linked in")
	}
}

// A directory plst created is a directory plst removes. One that someone has
// since put their own file in is theirs, and stays.
func TestOffRemovesADirectoryItCreatedButNotOneItFilled(t *testing.T) {
	_, agent := scratch(t)
	makeHarness(t, "rails", "", "agents", "skills")

	if _, err := Use("rails", User, func(string) {}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(agent, "skills", "later.md"), "ADDED AFTER\n")

	if _, err := Off(User, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(agent, "agents")); err == nil {
		t.Error("an empty directory plst created was left behind")
	}
	if b, _ := os.ReadFile(filepath.Join(agent, "skills", "later.md")); string(b) != "ADDED AFTER\n" {
		t.Errorf("a file added while the harness was applied is %q", b)
	}
}

// Replace is still available, and is right for a personal harness at user scope:
// what it provides is exactly what the agent reads.
func TestReplaceLinksTheDirectoryItself(t *testing.T) {
	_, agent := scratch(t)
	writeFile(t, filepath.Join(agent, "agents", "mine.md"), "MY AGENT\n")
	h := makeHarness(t, "rails", "", "agents")
	writeFile(t, filepath.Join(h.Dir, ManifestFile), `{"name":"rails","link":"replace"}`)

	rep, err := Use("rails", User, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Parked) != 1 || rep.Parked[0] != "agents" {
		t.Errorf("parked %v, want the whole directory", rep.Parked)
	}
	if fi, err := os.Lstat(filepath.Join(agent, "agents")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Error("agents is not a symlink")
	}
	if _, err := os.Stat(filepath.Join(agent, "agents", "mine.md")); err == nil {
		t.Error("the project's own agent is still visible under a replaced directory")
	}

	if _, err := Off(User, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(agent, "agents", "mine.md")); string(b) != "MY AGENT\n" {
		t.Errorf("after off, the original agent is %q", b)
	}
}

// A typo here decides whether someone's committed configuration gets moved
// aside, so it is refused rather than defaulted.
func TestUnknownLinkModeIsRefused(t *testing.T) {
	scratch(t)
	h := makeHarness(t, "rails", "", "agents")
	writeFile(t, filepath.Join(h.Dir, ManifestFile), `{"name":"rails","link":"merged"}`)

	if _, err := Open(h.Dir); err == nil {
		t.Fatal("a harness declaring an unknown link mode was accepted")
	}
}

// The settings file holds the model, the theme, the status line and hooks
// registered by hand. A harness owns what it declared and nothing else.
func TestHooksAreMergedAndRemovedExactly(t *testing.T) {
	_, agent := scratch(t)
	writeFile(t, filepath.Join(agent, "settings.json"), `{
  "model": "opus",
  "theme": "dark",
  "hooks": { "UserPromptSubmit": [ { "hooks": [ { "type": "command", "command": "/mine/emit" } ] } ] }
}`)
	makeHarness(t, "rails", `{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"`+RootVar+`/hooks/g.sh"}]}]}`, "hooks")

	if _, err := Use("rails", User, func(string) {}); err != nil {
		t.Fatal(err)
	}
	s, err := claude.LoadSettings(claude.SettingsPath())
	if err != nil {
		t.Fatal(err)
	}
	hooks, _ := s.Hooks()
	if len(claude.Commands(hooks["PreToolUse"])) != 1 {
		t.Errorf("the harness hook was not registered: %v", hooks)
	}
	if cmds := claude.Commands(hooks["UserPromptSubmit"]); len(cmds) != 1 || cmds[0] != "/mine/emit" {
		t.Errorf("a hand-registered hook was disturbed: %v", cmds)
	}

	if _, err := Off(User, func(string) {}); err != nil {
		t.Fatal(err)
	}
	s, _ = claude.LoadSettings(claude.SettingsPath())
	hooks, _ = s.Hooks()
	if _, ok := hooks["PreToolUse"]; ok {
		t.Error("the event was left behind with nothing under it")
	}
	if cmds := claude.Commands(hooks["UserPromptSubmit"]); len(cmds) != 1 {
		t.Errorf("the hand-registered hook did not survive removal: %v", cmds)
	}
	// Everything that is not hooks is passed through untouched.
	raw, err := os.ReadFile(claude.SettingsPath())
	if err != nil {
		t.Fatal(err)
	}
	var all map[string]any
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatal(err)
	}
	if all["model"] != "opus" || all["theme"] != "dark" {
		t.Errorf("other settings were lost: %v", all)
	}
}

// Swapping has to take the previous harness out, or its hooks stay registered.
func TestUseReplacesThePreviousHarness(t *testing.T) {
	_, agent := scratch(t)
	makeHarness(t, "one", `{"Stop":[{"hooks":[{"type":"command","command":"`+RootVar+`/hooks/a.sh"}]}]}`, "CLAUDE.md", "hooks")
	makeHarness(t, "two", `{"Stop":[{"hooks":[{"type":"command","command":"`+RootVar+`/hooks/b.sh"}]}]}`, "CLAUDE.md", "hooks")

	if _, err := Use("one", User, func(string) {}); err != nil {
		t.Fatal(err)
	}
	rep, err := Use("two", User, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Replaced != "one" {
		t.Errorf("replaced %q, want one", rep.Replaced)
	}
	s, _ := claude.LoadSettings(claude.SettingsPath())
	hooks, _ := s.Hooks()
	cmds := claude.Commands(hooks["Stop"])
	if len(cmds) != 1 || !strings.Contains(cmds[0], "/two/hooks/b.sh") {
		t.Errorf("Stop hooks = %v, want only the new harness's", cmds)
	}
	if b, _ := os.ReadFile(filepath.Join(agent, "CLAUDE.md")); string(b) != "# two\n" {
		t.Errorf("CLAUDE.md reads %q", b)
	}
}

// Applying the same harness twice must not double its hooks.
func TestUseIsIdempotent(t *testing.T) {
	scratch(t)
	makeHarness(t, "rails", `{"Stop":[{"hooks":[{"type":"command","command":"`+RootVar+`/hooks/a.sh"}]}]}`, "hooks")
	for range 3 {
		if _, err := Use("rails", User, func(string) {}); err != nil {
			t.Fatal(err)
		}
	}
	s, _ := claude.LoadSettings(claude.SettingsPath())
	hooks, _ := s.Hooks()
	if got := claude.Commands(hooks["Stop"]); len(got) != 1 {
		t.Errorf("registered %d copies of one hook: %v", len(got), got)
	}
}

// Rules are only ever read from a project. Linking one at user scope would look
// correct and never be read, so it is reported instead.
func TestRulesAreSkippedAtUserScope(t *testing.T) {
	_, agent := scratch(t)
	makeHarness(t, "rails", "", "CLAUDE.md", "rules")

	rep, err := Use("rails", User, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Skipped) != 1 || rep.Skipped[0] != "rules" {
		t.Errorf("skipped = %v, want rules", rep.Skipped)
	}
	if _, err := os.Lstat(filepath.Join(agent, "rules")); err == nil {
		t.Error("rules were linked at user scope")
	}

	// At project scope they are linked, because that is where they work.
	repo := t.TempDir()
	rep, err = Use("rails", ProjectScope(repo), func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Skipped) != 0 {
		t.Errorf("skipped %v at project scope", rep.Skipped)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, ".claude", "rules", "item.md")); string(b) != "item\n" {
		t.Errorf("rules were not linked at project scope: %q", b)
	}
}

// The two scopes are independent: a project runs one harness while everything
// else runs another.
func TestScopesAreTrackedSeparately(t *testing.T) {
	scratch(t)
	makeHarness(t, "one", "", "CLAUDE.md")
	makeHarness(t, "two", "", "CLAUDE.md")
	repo := t.TempDir()

	if _, err := Use("one", User, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := Use("two", ProjectScope(repo), func(string) {}); err != nil {
		t.Fatal(err)
	}
	state := LoadState()
	if a, _ := state.Active(User); a.Harness != "one" {
		t.Errorf("user scope has %q", a.Harness)
	}
	if a, _ := state.Active(ProjectScope(repo)); a.Harness != "two" {
		t.Errorf("project scope has %q", a.Harness)
	}
	// User scope is reported first: it is the one that applies everywhere.
	if order := state.Order(); len(order) != 2 || order[0] != User.Resolve() {
		t.Errorf("order = %v", order)
	}
}

// switchTo points plst at another agent directory the way `plst harness dir` does.
func switchTo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := claude.SetDir(dir); err != nil {
		t.Fatal(err)
	}
}

func isLink(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode()&os.ModeSymlink != 0
}

// The reason state is kept per directory. Switching to another one and applying a
// harness there must not take the first one's harness out from under it: that is
// how someone ends up with a directory that quietly lost its configuration.
func TestEachAgentDirectoryKeepsItsOwnHarness(t *testing.T) {
	_, a := scratch(t)
	b := filepath.Join(t.TempDir(), "other")
	makeHarness(t, "one", "", "CLAUDE.md")
	makeHarness(t, "two", "", "CLAUDE.md")

	if _, err := Use("one", User, func(string) {}); err != nil {
		t.Fatal(err)
	}
	switchTo(t, b)
	if _, ok := LoadState().Active(User); ok {
		t.Fatal("the new directory reports a harness it was never given")
	}
	rep, err := Use("two", User, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Replaced != "" {
		t.Errorf("applying in a second directory replaced %q", rep.Replaced)
	}
	if got, _ := rep.Scope.IsUser(); got != b {
		t.Errorf("report names %q, want the directory it acted on, %q", got, b)
	}

	if !isLink(filepath.Join(a, "CLAUDE.md")) {
		t.Error("the first directory lost its harness when another was applied")
	}
	state := LoadState()
	if h, _ := state.Active(UserScope(a)); h.Harness != "one" {
		t.Errorf("first directory has %q", h.Harness)
	}
	if h, _ := state.Active(UserScope(b)); h.Harness != "two" {
		t.Errorf("second directory has %q", h.Harness)
	}

	// Off acts on the directory in use and nothing else.
	if _, err := Off(User, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if isLink(filepath.Join(b, "CLAUDE.md")) {
		t.Error("off left the harness in the directory it was run for")
	}
	if !isLink(filepath.Join(a, "CLAUDE.md")) {
		t.Error("off reached into a directory that was not in use")
	}
	switchTo(t, a)
	if _, err := Off(User, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if isLink(filepath.Join(a, "CLAUDE.md")) {
		t.Error("the first directory's harness was not removed once it was in use again")
	}
}

// An `off` in a directory with nothing applied has to say where the harness went,
// or it reads as though it was lost.
func TestOffSaysWhereTheHarnessIs(t *testing.T) {
	_, a := scratch(t)
	makeHarness(t, "one", "", "CLAUDE.md")
	if _, err := Use("one", User, func(string) {}); err != nil {
		t.Fatal(err)
	}
	switchTo(t, filepath.Join(t.TempDir(), "other"))

	_, err := Off(User, func(string) {})
	if err == nil {
		t.Fatal("off succeeded in a directory with nothing applied")
	}
	if !strings.Contains(err.Error(), "one is applied at "+a) {
		t.Errorf("error does not say where it is: %v", err)
	}
}

// State written before user scope was kept per directory sits under a bare
// "user" key. It belongs to the directory it was applied to, and dropping it
// would leave links that nothing knows how to remove.
func TestLegacyUserStateIsMigratedToItsDirectory(t *testing.T) {
	_, a := scratch(t)
	makeHarness(t, "one", "", "CLAUDE.md")
	if _, err := Use("one", User, func(string) {}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(statePath())
	if err != nil {
		t.Fatal(err)
	}
	legacy := strings.Replace(string(b), `"`+string(UserScope(a))+`"`, `"user"`, 1)
	if legacy == string(b) {
		t.Fatal("did not find the scope key to make legacy")
	}
	writeFile(t, statePath(), legacy)
	switchTo(t, filepath.Join(t.TempDir(), "other"))

	state := LoadState()
	if h, ok := state.Active(UserScope(a)); !ok || h.Harness != "one" {
		t.Fatalf("legacy state was not moved to %s: %+v", a, state.Scopes)
	}
	if _, ok := state.Scopes[User]; ok {
		t.Error("the bare user key is still there")
	}
	// And it can still be taken out, from the directory it belongs to.
	switchTo(t, a)
	if _, err := Off(User, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if isLink(filepath.Join(a, "CLAUDE.md")) {
		t.Error("the migrated harness was not removed")
	}
}

func TestParseSource(t *testing.T) {
	cases := map[string][3]string{
		"esmarkowski/rails-harness":                 {"https://github.com/esmarkowski/rails-harness.git", "rails", ""},
		"https://github.com/o/plasticity-harness-x": {"https://github.com/o/plasticity-harness-x.git", "x", ""},
		"git@github.com:o/r.git":                    {"https://github.com/o/r.git", "r", ""},
		"o/r@v2":                                    {"https://github.com/o/r.git", "r", "v2"},

		// Anywhere but github. A pin is exactly where a company's own host shows
		// up, so these are taken as written rather than refused.
		"git@gitlab.example.com:team/tools-harness.git": {"git@gitlab.example.com:team/tools-harness.git", "tools", ""},
		"ssh://git@example.com/team/r.git":              {"ssh://git@example.com/team/r.git", "r", ""},
		"/srv/git/m1-harness":                           {"/srv/git/m1-harness", "m1", ""},
		"/srv/git/m1-harness@v1":                        {"/srv/git/m1-harness", "m1", "v1"},
	}
	for in, want := range cases {
		url, name, ref, err := parseSource(in)
		if err != nil {
			t.Errorf("parseSource(%q): %v", in, err)
			continue
		}
		if url != want[0] || name != want[1] || ref != want[2] {
			t.Errorf("parseSource(%q) = %q %q %q, want %q", in, url, name, ref, want)
		}
	}
	for _, bad := range []string{"", "r", "a/b/c"} {
		if _, _, _, err := parseSource(bad); err == nil {
			t.Errorf("parseSource(%q) was accepted", bad)
		}
	}
}

// gitRun runs git in a directory and fails the test if it complains. The pin
// exists to move a harness onto a ref, and there is no way to test that without
// letting git actually do it.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{
		"-C", dir,
		"-c", "user.name=test",
		"-c", "user.email=test@example.com",
		"-c", "commit.gpgsign=false",
	}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// A repository's own plst.json may already declare the modules it builds. This
// program knows about one key and must not drop the rest.
func TestPinRoundTripsAndPreservesOtherKeys(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, PinFile), `{"modules":[{"name":"x","build":"go build"}]}`)

	if _, err := SetPin(root, "o/r@v2"); err != nil {
		t.Fatal(err)
	}
	p, ok, err := LoadPin(root)
	if err != nil || !ok {
		t.Fatalf("LoadPin = %v, %v", ok, err)
	}
	if p.Source != "https://github.com/o/r.git" || p.Ref != "v2" {
		t.Errorf("pin = %+v", p)
	}
	if name, err := p.HarnessName(); err != nil || name != "r" {
		t.Errorf("HarnessName = %q, %v", name, err)
	}
	b, _ := os.ReadFile(filepath.Join(root, PinFile))
	if !strings.Contains(string(b), `"modules"`) {
		t.Errorf("the repository's own modules key was dropped: %s", b)
	}
}

// Most repositories pin nothing, and that is not an error.
func TestLoadPinIsAbsentWithoutTheKey(t *testing.T) {
	root := t.TempDir()
	if _, ok, err := LoadPin(root); ok || err != nil {
		t.Errorf("with no file: %v, %v", ok, err)
	}
	writeFile(t, filepath.Join(root, PinFile), `{"modules":[]}`)
	if _, ok, err := LoadPin(root); ok || err != nil {
		t.Errorf("with no harness key: %v, %v", ok, err)
	}
}

// A pin that only names a harness works on the machine that already installed it
// and nowhere else, which defeats the point of committing one.
func TestLoadPinRequiresASource(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, PinFile), `{"harness":{"ref":"v1"}}`)
	if _, _, err := LoadPin(root); err == nil {
		t.Fatal("a pin naming no source was accepted")
	}
}

// The whole point: a checkout gets the harness the repository says, at the
// version it says, from one command that is safe to run twice.
func TestSyncMovesToThePinnedRefAndApplies(t *testing.T) {
	scratch(t)
	harnesses, err := Root()
	if err != nil {
		t.Fatal(err)
	}

	// A harness repository tagged v1, with a later commit on the branch — so
	// following the branch and honouring the pin give different answers.
	src := filepath.Join(t.TempDir(), "src")
	writeFile(t, filepath.Join(src, "CLAUDE.md"), "V1\n")
	writeFile(t, filepath.Join(src, ManifestFile), `{"name":"pinned"}`)
	writeFile(t, filepath.Join(src, "agents", "item.md"), "item\n")
	gitRun(t, src, "init", "-q", "-b", "main")
	gitRun(t, src, "add", "-A")
	gitRun(t, src, "commit", "-q", "-m", "one")
	gitRun(t, src, "tag", "v1")
	writeFile(t, filepath.Join(src, "CLAUDE.md"), "V2\n")
	gitRun(t, src, "add", "-A")
	gitRun(t, src, "commit", "-q", "-m", "two")

	// Already installed, on the branch head, as a second developer's machine
	// would be after cloning it once.
	gitRun(t, src, "clone", "-q", src, filepath.Join(harnesses, "pinned"))

	repo := t.TempDir()
	if err := WritePin(repo, Pin{Source: src, Ref: "v1", Name: "pinned"}); err != nil {
		t.Fatal(err)
	}

	rep, err := Sync(repo, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rep.Action, "moved") {
		t.Errorf("action = %q, want it to have moved onto the pinned ref", rep.Action)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, ".claude", "CLAUDE.md")); string(b) != "V1\n" {
		t.Errorf("CLAUDE.md reads %q, want the pinned ref rather than the branch head", b)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, ".claude", "agents", "item.md")); string(b) != "item\n" {
		t.Errorf("the harness was not applied at project scope: %q", b)
	}
	if a, ok := LoadState().Active(ProjectScope(repo)); !ok || a.Harness != "pinned" {
		t.Errorf("state = %+v, %v", a, ok)
	}

	// Safe to run twice, which is what makes it usable in bin/setup and CI.
	again, err := Sync(repo, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if again.Action != "already current" {
		t.Errorf("second sync action = %q", again.Action)
	}
}

// A same-named harness from somewhere else would apply cleanly and be the wrong
// configuration — the exact thing a pin exists to prevent, so it is said out loud.
func TestSyncRefusesAHarnessFromADifferentSource(t *testing.T) {
	scratch(t)
	harnesses, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "src")
	writeFile(t, filepath.Join(src, ManifestFile), `{"name":"pinned"}`)
	writeFile(t, filepath.Join(src, "agents", "item.md"), "item\n")
	gitRun(t, src, "init", "-q", "-b", "main")
	gitRun(t, src, "add", "-A")
	gitRun(t, src, "commit", "-q", "-m", "one")
	gitRun(t, src, "clone", "-q", src, filepath.Join(harnesses, "pinned"))

	repo := t.TempDir()
	if err := WritePin(repo, Pin{Source: "someone/else", Name: "pinned"}); err != nil {
		t.Fatal(err)
	}
	_, err = Sync(repo, func(string) {})
	if err == nil {
		t.Fatal("a harness from a different source was accepted")
	}
	if !strings.Contains(err.Error(), "pins") {
		t.Errorf("error does not explain the mismatch: %v", err)
	}
}

// Nothing to sync is a clear message, not a crash.
func TestSyncWithoutAPinSaysSo(t *testing.T) {
	scratch(t)
	_, err := Sync(t.TempDir(), func(string) {})
	if err == nil || !strings.Contains(err.Error(), PinFile) {
		t.Fatalf("err = %v, want it to name %s", err, PinFile)
	}
}
