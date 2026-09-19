# plasticity-modules

Modules for [`plst`](https://github.com/esmarkowski/plasticity).

```sh
plst install esmarkowski/plasticity-modules
```

One repo, several module binaries. `plst.json` declares them all, so a single
install registers every subcommand.

| module | what it does |
|---|---|
| `harness` | interchangeable sets of agent configuration |

## harness

A harness is a directory — usually a git repository — holding the parts an agent
reads. Using one links those parts into place and registers its hooks; using
another swaps them.

```sh
plst harness new rails                       # scaffold one
plst harness install esmarkowski/my-harness  # or clone one
plst harness use rails                       # link it in
plst harness use rails --project             # for this repository only
plst harness off                             # put back what it displaced
plst harness list
plst harness dir ~/.claude-work             # which agent directory; switch to another

plst harness pin esmarkowski/my-harness@v1   # record what this repository expects
plst harness sync                            # install it, move to the ref, apply
```

### Layout

```
rails/
  harness.json     name, description, link mode, hook declarations
  CLAUDE.md
  rules/           project scope only
  agents/
  skills/
  commands/
  hooks/           scripts, registered from harness.json
```

Components are read from the directory, not declared — adding `agents/` to a
harness is enough to have it linked.

### Symlinks, not copies

Each component becomes a symlink into the harness. Swapping is instant, nothing
drifts, and an edit made while a harness is active is an edit to the harness, so
its own git history is the record of it.

This was checked rather than assumed. A scratch project with a symlinked
`CLAUDE.md`, a symlinked `agents/` directory, and an `agents/` directory holding a
symlinked entry were each probed for what the agent reported loading. All three
loaded — directory links and entry links alike.

Only the known component paths are ever touched. The agent's configuration
directory also holds live state — a daemon lock, job directories, a history file,
plan files, caches — and none of it is a harness's business.

Anything real already at a component path is **moved aside, never deleted**, into
`~/.plasticity/harnesses/.parked/`, and put back by `plst harness off`.

### Merging into what is already there

A directory component is linked **entry by entry**, so the directory itself stays
real:

```
.claude/agents/reviewer-rails.md      the repository's own, committed, untouched
.claude/agents/motion1-review.md  ->  ~/.plasticity/harnesses/motion1/agents/motion1-review.md
```

This is the default, and it is the difference between a harness a team can use and
one only its author can. A repository commits agents, skills, and rules of its own;
linking over the whole directory made every one of those files read as **deleted**
in `git status`, so using a harness meant a dirty working tree and a symlink into
someone's home directory one `git add -A` away from being committed.

Only a **same-named** entry is still displaced, and git reports that as a
typechange rather than a deletion. A dotted entry — the `.gitkeep` that carries an
empty directory through git — is not linked at all: it is the harness's own
bookkeeping, not something the agent reads.

A harness that wants the old behaviour says so:

```json
{ "name": "mine", "link": "replace" }
```

`replace` links the directory itself and parks whatever was there. It is right for
a personal harness at user scope, where what the harness provides should be exactly
what the agent reads and anything left over is a surprise. It is wrong for a
project's. An unrecognised value is refused rather than defaulted — the choice
decides whether someone's committed files get moved aside, so it is not a thing to
guess at.

A file component has no entries and is always the whole of itself.

### Rules are project scope only

The agent never reads rules from user scope. Measured: thirteen rule loads on the
machine this was written on, every one from a project's own `.claude/rules`, none
from `~/.claude/rules`. So `use` skips `rules/` at user scope and says so, rather
than creating a link that would look right and never be read.

Rules also load on `path_glob_match` — the `paths:` frontmatter has to match a
file touched during the session — so they are not loaded at session start at all.

### Hooks

Declared in `harness.json`, in the same shape the agent's own settings file uses.
Use `${HARNESS_ROOT}` rather than writing down a path, the same idea as the
agent's `${CLAUDE_PLUGIN_ROOT}`: a harness may be cloned anywhere, so a hook
command that names a path works on one machine.

```json
{
  "name": "rails",
  "hooks": {
    "PreToolUse": [
      { "matcher": "Bash",
        "hooks": [{ "type": "command", "command": "${HARNESS_ROOT}/hooks/guard.sh" }] }
    ]
  }
}
```

Registrations are **merged**, not replaced. The settings file also holds the
model, the theme, the status line, enabled plugins, and any hooks you registered
by hand — a harness owns what it declared and nothing else. What was written is
recorded, so `off` removes exactly that rather than guessing.

Fields this program has never heard of — a `timeout`, a `statusMessage` — are
passed through untouched.

### Pinning one to a repository

`use` records what it did in machine-local state, keyed by absolute path. That is
enough for one person on one checkout and no use at all for a team: a fresh clone
has no binding, two developers cannot be shown to be running the same
configuration, and every git worktree is a different absolute path and therefore a
separate scope — a repository with sixteen worktrees would need sixteen `use` runs.

A pin is committed, so all of them inherit it:

```json
{
  "harness": {
    "source": "EnovisHCS/motion1-harness",
    "ref": "v1.4.0"
  }
}
```

It lives in `plst.json`, the same file plst reads when a repository *provides*
modules, under a different key: one file for a repository's relationship to plst,
whichever direction it runs in. A second filename for the second meaning is how
this gets confusing.

```sh
plst harness pin EnovisHCS/motion1-harness@v1.4.0   # writes it
git add plst.json && git commit
plst harness sync                                   # every other checkout
```

`sync` installs the harness if it is missing, moves it onto the pinned ref, and
applies it at project scope. It is idempotent, so it belongs in `bin/setup`, a
postinstall hook, and CI.

**Pin a ref.** Without one, "the same harness" is not the same harness — one
person running `update` moves ahead of everyone else and nothing says so. `pin`
warns when you leave it out.

A harness already installed under that name from a **different** source is
refused, not used. That case would apply cleanly and be the wrong configuration,
which is the exact thing a pin exists to prevent.

Sources are not limited to github. `owner/repo` is normalised, and anything else
git can clone — an ssh URL, a self-hosted remote, a local path — is taken as
written, because a pin is where a company's own host shows up.

### Scopes

`user` is the agent's own configuration directory and is the default: a harness is
normally the whole way you work. `--project` acts on a repository's `.claude`,
found by walking up to the repository root, so it lands there rather than in
whichever subdirectory you typed the command in. The two are tracked
independently, so one project can run a different harness from everything else.

### Which agent directory

`~/.claude` unless plst's config says otherwise. Switching is one command, and the
current one is never a guess:

```sh
plst harness dir                 # which one is in use, why, and what it holds
plst harness dir ~/.claude-work  # switch (stored as claude_dir in plst's config)
plst harness dir --reset         # back to ~/.claude
plst harness dir --path          # just the path, for scripts
```

`dir` also lists every other directory that has a harness applied. `list` opens
with the directory in use, and `use` and `off` name the directory they acted on,
so a forgotten switch shows up on the next command instead of three weeks later.

**Each directory keeps its own harness.** Applying one in `~/.claude-work` does not
touch what `~/.claude` has, and `off` only ever acts on the directory `dir` shows.
`off` in a directory with nothing applied says where the harness is.

Switching moves where *plst* applies things. It cannot move the agent, which reads
`~/.claude` unless it is started with `CLAUDE_CONFIG_DIR`. Keep the two together by
letting plst say where:

```sh
alias claude='CLAUDE_CONFIG_DIR=$(plst harness dir --path) command claude'
```

`CLAUDE_CONFIG_DIR` itself is not read. If it is set in the shell to somewhere other
than the directory plst is using, `dir`, `list` and `use` say so.

The setting can also be edited by hand: `{ "claude_dir": "~/.claude-work" }` in
`~/.plasticity/config.json` (`plst where` prints the path). `~` is your home
directory, and a relative path is relative to plst's home.
