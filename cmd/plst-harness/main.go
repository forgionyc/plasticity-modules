// Command plst-harness is a plst module for managing interchangeable sets of
// agent configuration.
//
// A harness is a directory — usually a git repository — holding the parts an
// agent reads: CLAUDE.md, rules, agents, skills, commands, hooks. Using one links
// those parts into place and registers its hooks; using another swaps them.
package main

import (
	"fmt"
	"os"
)

var version = "dev"

const usage = `plst harness — interchangeable sets of agent configuration

  plst harness list                     what is installed, and what is applied
  plst harness install <owner>/<repo>   clone a harness
  plst harness new <name>               scaffold one
  plst harness use <name> [--project]   link it into place and register its hooks
  plst harness off [--project]          take it out, restore what it displaced
  plst harness show <name>              what a harness contains
  plst harness update <name>            pull it
  plst harness remove <name>            delete it

  plst harness pin <owner>/<repo>[@ref]  record the harness this repository expects
  plst harness sync                      install, move to the pinned ref, apply

  plst harness dir                       which agent directory is in use, and what it holds
  plst harness dir <path>                switch to another one
  plst harness dir --reset               back to the default, ~/.claude
  plst harness dir --path                just the path: CLAUDE_CONFIG_DIR=$(plst harness dir --path) claude

  --project   act on this repository's .claude instead of the agent's own.
              Rules only work here: the agent never reads them from user scope.

Each agent directory keeps its own harness: switching with dir leaves the one you
left exactly as it was, and use and off only ever touch the directory shown by dir.

A pin lives in plst.json and is committed, so every clone and every worktree of a
repository gets the same harness at the same ref. Syncing is safe to run twice.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "list", "ls":
		os.Exit(list(args))
	case "install":
		os.Exit(install(args))
	case "new":
		os.Exit(newHarness(args))
	case "use":
		os.Exit(use(args))
	case "off", "unuse":
		os.Exit(off(args))
	case "show":
		os.Exit(show(args))
	case "update":
		os.Exit(update(args))
	case "remove", "uninstall":
		os.Exit(remove(args))
	case "dir":
		os.Exit(agentDir(args))
	case "pin":
		os.Exit(pin(args))
	case "sync":
		os.Exit(sync(args))
	case manifestFlag:
		os.Exit(manifest())
	case "--version", "version":
		fmt.Println("plst-harness " + version)
		os.Exit(0)
	case "--help", "-h", "help":
		fmt.Print(usage)
		os.Exit(0)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

// hasFlag reports whether a flag was given.
func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

// positional is the first argument that is not a flag.
func positional(args []string) string {
	for _, a := range args {
		if len(a) > 0 && a[0] != '-' {
			return a
		}
	}
	return ""
}
