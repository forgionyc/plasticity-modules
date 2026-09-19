package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// manifestFlag is how plst asks a module what it offers. A flag rather than a
// subcommand, so it can never collide with a command this module wants to own.
const manifestFlag = "--plst-manifest"

type command struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// manifest tells plst what this module is. stdout and nothing else: plst reads it
// as JSON, so a stray diagnostic would be a parse failure rather than a warning.
func manifest() int {
	out := struct {
		Name        string    `json:"name"`
		Description string    `json:"description"`
		Version     string    `json:"version"`
		Commands    []command `json:"commands"`
	}{
		Name:        "harness",
		Description: "interchangeable sets of agent configuration",
		Version:     version,
		Commands: []command{
			{"list", "what is installed, and what is applied"},
			{"install", "clone a harness from a repo"},
			{"new", "scaffold a harness"},
			{"use", "link a harness into place"},
			{"off", "take a harness out"},
			{"dir", "show or switch the agent directory"},
			{"show", "what a harness contains"},
			{"update", "pull a harness"},
			{"remove", "delete a harness"},
		},
	}
	b, err := json.Marshal(out)
	if err != nil {
		return 1
	}
	fmt.Fprintln(os.Stdout, string(b))
	return 0
}
