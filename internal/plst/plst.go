// Package plst reads the environment plst hands a module.
//
// A module is told where things are rather than working it out again: plst owns
// that decision, and a module that derives its own paths is a second
// implementation of config that will disagree with the first the moment either
// changes. The fallbacks exist only so a module still runs when invoked directly,
// which is how it gets developed.
package plst

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	EnvHome      = "PLST_HOME"
	EnvModuleDir = "PLST_MODULE_DIR"
	EnvConfig    = "PLST_CONFIG"
	EnvTools     = "PLST_TOOLS"
	EnvBin       = "PLST_BIN"
)

// Home is the root of plst's state, resolved the same way plst resolves it.
func Home() string {
	if h := os.Getenv(EnvHome); h != "" {
		return h
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "plasticity")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".plasticity"
	}
	return filepath.Join(home, ".plasticity")
}

// ConfigPath is plst's config file, resolved the same way plst resolves it.
func ConfigPath() string {
	if p := os.Getenv(EnvConfig); p != "" {
		return p
	}
	return filepath.Join(Home(), "config.json")
}

// Config is the part of plst's config file that modules read; plst ignores the keys it does not know.
type Config struct {
	ClaudeDir string `json:"claude_dir"`
}

// LoadConfig reads plst's config file; missing or corrupt is empty, as plst treats it.
func LoadConfig() Config {
	var c Config
	if b, err := os.ReadFile(ConfigPath()); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

// SetClaudeDir writes claude_dir into plst's config file, or removes it when dir
// is empty. Every other key is left as it was, and a file that is not valid JSON
// is refused rather than overwritten.
func SetClaudeDir(dir string) error {
	path := ConfigPath()
	raw := map[string]json.RawMessage{}
	switch b, err := os.ReadFile(path); {
	case os.IsNotExist(err):
	case err != nil:
		return err
	case len(bytes.TrimSpace(b)) > 0:
		if err := json.Unmarshal(b, &raw); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	if dir == "" {
		delete(raw, "claude_dir")
	} else {
		v, err := json.Marshal(dir)
		if err != nil {
			return err
		}
		raw["claude_dir"] = v
	}
	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".plst-new"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Dir is a subdirectory of plst's state, created on demand.
func Dir(name string) (string, error) {
	d := filepath.Join(Home(), name)
	return d, os.MkdirAll(d, 0o755)
}
