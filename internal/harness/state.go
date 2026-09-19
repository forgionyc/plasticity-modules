package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/esmarkowski/plasticity-modules/internal/claude"
	"github.com/esmarkowski/plasticity-modules/internal/plst"
)

// Scope is where a harness is applied.
//
// Two kinds, because the agent reads two: its own configuration directory, and a
// repository's. They are tracked separately so a project can run one harness
// while everything else runs another.
type Scope string

const (
	// User is the agent's own configuration directory, whichever one plst is
	// pointed at right now. Resolve turns it into that directory's own scope.
	User Scope = "user"
)

// UserScope names one agent directory's scope. There is one per directory, as
// there is one per repository: switching directories means two agents to
// configure, and a harness applied to one is not applied to the other.
func UserScope(dir string) Scope { return Scope("user:" + dir) }

// IsUser reports whether a scope is one agent directory's, and which.
func (s Scope) IsUser() (string, bool) {
	const p = "user:"
	if len(s) > len(p) && string(s[:len(p)]) == p {
		return string(s[len(p):]), true
	}
	return "", false
}

// Resolve turns User into the scope of the directory plst is pointed at now; any
// other scope is already itself.
func (s Scope) Resolve() Scope {
	if s == User {
		return UserScope(claude.Dir())
	}
	return s
}

// ProjectScope names a repository's scope by its root, since there are as many
// project scopes as there are repositories.
func ProjectScope(root string) Scope { return Scope("project:" + root) }

// IsProject reports whether a scope is a repository's, and where.
func (s Scope) IsProject() (string, bool) {
	const p = "project:"
	if len(s) > len(p) && string(s[:len(p)]) == p {
		return string(s[len(p):]), true
	}
	return "", false
}

// Label is the scope as a person would say it.
func (s Scope) Label() string {
	if root, ok := s.IsProject(); ok {
		return "project " + root
	}
	if dir, ok := s.IsUser(); ok {
		return "user " + dir
	}
	return "user"
}

// Applied is a harness in place, and everything that was done to put it there.
//
// Recorded rather than inferred. Reverting has to undo exactly what was done —
// the same links, the same hook registrations — and a revert that works out what
// it probably did is a revert that eventually removes something it did not add.
type Applied struct {
	Harness string    `json:"harness"`
	Dir     string    `json:"dir"`
	At      time.Time `json:"at"`
	Links   []Link    `json:"links,omitempty"`
	// Dirs are directories this program created to hold entry links. Recorded so
	// reverting can remove them, and only while they are empty: a directory
	// someone has since put their own file in is theirs.
	Dirs []string `json:"dirs,omitempty"`
	// Commands are the expanded hook commands written into the settings file.
	// Matched on the string when removing, not on position: an index shifts the
	// moment anything else registers a hook.
	Commands []string `json:"commands,omitempty"`
	Settings string   `json:"settings,omitempty"`
}

// Link is one component put in place.
type Link struct {
	Path   string `json:"path"`
	Target string `json:"target"`
	// Parked is where a real file or directory already at Path was moved to.
	// Empty when there was nothing there. Nothing is ever deleted: what was in
	// the agent's directory before plst arrived is not plst's to throw away.
	Parked string `json:"parked,omitempty"`
}

// State is what is applied where.
type State struct {
	Scopes map[Scope]Applied `json:"scopes"`
}

func statePath() string { return filepath.Join(plst.Home(), "harness-state.json") }

// LoadState reads the state, which is empty on a machine that has never used a
// harness.
func LoadState() State {
	s := State{Scopes: map[Scope]Applied{}}
	b, err := os.ReadFile(statePath())
	if err != nil {
		return s
	}
	_ = json.Unmarshal(b, &s)
	if s.Scopes == nil {
		s.Scopes = map[Scope]Applied{}
	}
	// Applied before user scope was kept per directory: it belongs to the
	// directory it was applied to, and dropping it would strand its links.
	if a, ok := s.Scopes[User]; ok {
		delete(s.Scopes, User)
		dir := claude.Dir()
		if a.Settings != "" {
			dir = filepath.Dir(a.Settings)
		}
		if _, taken := s.Scopes[UserScope(dir)]; !taken {
			s.Scopes[UserScope(dir)] = a
		}
	}
	return s
}

// Save writes the state.
func (s State) Save() error {
	if err := os.MkdirAll(plst.Home(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(), append(b, '\n'), 0o600)
}

// Active is the harness applied in a scope.
func (s State) Active(scope Scope) (Applied, bool) {
	a, ok := s.Scopes[scope.Resolve()]
	return a, ok
}

// Order lists the scopes in a stable order, user first: it is the one that
// applies everywhere, so it is the one to read first.
func (s State) Order() []Scope {
	out := make([]Scope, 0, len(s.Scopes))
	for k := range s.Scopes {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		_, iu := out[i].IsUser()
		_, ju := out[j].IsUser()
		if iu != ju {
			return iu
		}
		return out[i] < out[j]
	})
	return out
}

// elsewhere names the other agent directories that do have a harness, for an
// error that would otherwise leave someone wondering where theirs went.
func (s State) elsewhere(scope Scope) string {
	if _, ok := scope.IsUser(); !ok {
		return ""
	}
	var parts []string
	for _, sc := range s.Order() {
		if dir, ok := sc.IsUser(); ok && sc != scope {
			parts = append(parts, s.Scopes[sc].Harness+" is applied at "+dir)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " — " + strings.Join(parts, "; ")
}

// parkDir is where a displaced file goes: under plst's own state, stamped, so two
// swaps never collide and nothing is ever overwritten.
func parkDir(stamp string) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	d := filepath.Join(root, ".parked", stamp)
	return d, os.MkdirAll(d, 0o755)
}
