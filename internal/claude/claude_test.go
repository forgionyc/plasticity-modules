package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/esmarkowski/plasticity-modules/internal/plst"
)

// isolate points plst's state and the user's home at temporary directories and
// writes config as plst's config file, returning both.
func isolate(t *testing.T, config string) (plstHome, userHome string) {
	t.Helper()
	plstHome, userHome = t.TempDir(), t.TempDir()
	cfg := filepath.Join(plstHome, "config.json")
	if config != "" {
		if err := os.WriteFile(cfg, []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(plst.EnvHome, plstHome)
	t.Setenv(plst.EnvConfig, cfg)
	t.Setenv("HOME", userHome)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	return plstHome, userHome
}

func TestDirFromConfig(t *testing.T) {
	plstHome, userHome := isolate(t, "")
	abs := filepath.Join(t.TempDir(), "agent")

	cases := []struct {
		name, config, want string
	}{
		{"absolute", `{"claude_dir": "` + abs + `"}`, abs},
		{"home shorthand", `{"claude_dir": "~/work/.claude"}`, filepath.Join(userHome, "work", ".claude")},
		{"bare home", `{"claude_dir": "~"}`, userHome},
		{"relative to plst home", `{"claude_dir": "agent"}`, filepath.Join(plstHome, "agent")},
		{"unset", `{"tools": ["claude"]}`, filepath.Join(userHome, ".claude")},
		{"empty", `{"claude_dir": ""}`, filepath.Join(userHome, ".claude")},
		{"corrupt", `{"claude_dir": `, filepath.Join(userHome, ".claude")},
		{"wrong type", `{"claude_dir": 5}`, filepath.Join(userHome, ".claude")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := os.WriteFile(plst.ConfigPath(), []byte(c.config), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := Dir(); got != c.want {
				t.Fatalf("Dir() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestDirMissingConfig(t *testing.T) {
	_, userHome := isolate(t, "")
	if got, want := Dir(), filepath.Join(userHome, ".claude"); got != want {
		t.Fatalf("Dir() = %q, want %q", got, want)
	}
}

func TestDirIgnoresAgentEnvVar(t *testing.T) {
	_, userHome := isolate(t, `{"claude_dir": "~/configured"}`)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "from-env"))
	if got, want := Dir(), filepath.Join(userHome, "configured"); got != want {
		t.Fatalf("Dir() = %q, want %q: the setting is the only source", got, want)
	}

	_, userHome = isolate(t, "")
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "from-env"))
	if got, want := Dir(), filepath.Join(userHome, ".claude"); got != want {
		t.Fatalf("Dir() = %q, want %q: the env var must not be read", got, want)
	}
}

func TestSettingsPathsFollowDir(t *testing.T) {
	isolate(t, `{"claude_dir": "~/cfg"}`)
	if got, want := SettingsPath(), filepath.Join(Dir(), "settings.json"); got != want {
		t.Fatalf("SettingsPath() = %q, want %q", got, want)
	}
}

func TestSetDirStoresAnAbsolutePathAndKeepsOtherKeys(t *testing.T) {
	plstHome, userHome := isolate(t, `{"tools": ["claude"], "module_dir": "mods"}`)
	wd := t.TempDir()
	t.Chdir(wd)

	cases := []struct{ arg, want string }{
		{"~/.claude-work", filepath.Join(userHome, ".claude-work")},
		{"rel/agent", filepath.Join(wd, "rel", "agent")},
		{"/abs/agent/", "/abs/agent"},
	}
	for _, c := range cases {
		got, err := SetDir(c.arg)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("SetDir(%q) = %q, want %q", c.arg, got, c.want)
		}
		if Dir() != c.want {
			t.Errorf("after SetDir(%q), Dir() = %q, want %q", c.arg, Dir(), c.want)
		}
	}

	b, err := os.ReadFile(filepath.Join(plstHome, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var kept map[string]json.RawMessage
	if err := json.Unmarshal(b, &kept); err != nil {
		t.Fatal(err)
	}
	if string(kept["module_dir"]) != `"mods"` || !strings.Contains(string(kept["tools"]), "claude") {
		t.Errorf("SetDir dropped other keys: %s", b)
	}

	if err := ResetDir(); err != nil {
		t.Fatal(err)
	}
	if Configured() || Dir() != filepath.Join(userHome, ".claude") {
		t.Errorf("after ResetDir: configured=%v dir=%q", Configured(), Dir())
	}
	b, _ = os.ReadFile(filepath.Join(plstHome, "config.json"))
	if strings.Contains(string(b), "claude_dir") || !strings.Contains(string(b), "module_dir") {
		t.Errorf("ResetDir should remove only claude_dir: %s", b)
	}
}

func TestSetDirCreatesTheConfigFile(t *testing.T) {
	plstHome, _ := isolate(t, "")
	if _, err := SetDir("/abs/agent"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(plstHome, "config.json")); err != nil {
		t.Fatal(err)
	}
	if Dir() != "/abs/agent" {
		t.Errorf("Dir() = %q", Dir())
	}
}

// A config file that cannot be read is not one to overwrite: it may hold
// settings that only look corrupt because someone is halfway through editing it.
func TestSetDirRefusesACorruptConfig(t *testing.T) {
	plstHome, _ := isolate(t, `{"module_dir": `)
	if _, err := SetDir("/abs/agent"); err == nil {
		t.Fatal("SetDir overwrote a config file it could not parse")
	}
	if b, _ := os.ReadFile(filepath.Join(plstHome, "config.json")); string(b) != `{"module_dir": ` {
		t.Errorf("the corrupt file was modified: %q", b)
	}
}

func TestSetDirRefusesAFile(t *testing.T) {
	isolate(t, "")
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SetDir(f); err == nil {
		t.Fatal("SetDir accepted a file as the agent directory")
	}
	if Configured() {
		t.Error("a refused directory was still stored")
	}
}

func TestEnvOverrideOnlyWhenItDisagrees(t *testing.T) {
	_, userHome := isolate(t, `{"claude_dir": "~/a"}`)
	if _, ok := EnvOverride(); ok {
		t.Error("an unset variable is not an override")
	}
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(userHome, "a")+"/")
	if _, ok := EnvOverride(); ok {
		t.Error("the same directory spelled differently is not an override")
	}
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(userHome, "b"))
	if got, ok := EnvOverride(); !ok || got != filepath.Join(userHome, "b") {
		t.Errorf("EnvOverride() = %q, %v", got, ok)
	}
}
