package main

import (
	"fmt"
	"os"

	"github.com/esmarkowski/plasticity-modules/internal/claude"
	"github.com/esmarkowski/plasticity-modules/internal/harness"
	"github.com/esmarkowski/plasticity-modules/internal/plst"
	"github.com/esmarkowski/plasticity-modules/internal/ui"
)

// agentDir shows which agent directory plst is using, or switches it.
func agentDir(args []string) int {
	path, reset, bare := positional(args), hasFlag(args, "--reset"), hasFlag(args, "--path")
	n := 0
	for _, on := range []bool{path != "", reset, bare} {
		if on {
			n++
		}
	}
	if n > 1 {
		ui.Fail(os.Stderr, fmt.Errorf("usage: plst harness dir [<path> | --reset | --path]"))
		return 2
	}
	switch {
	case bare:
		fmt.Println(claude.Dir())
		return 0
	case reset:
		return resetDir()
	case path != "":
		return setDir(path)
	}
	printDir()
	state := harness.LoadState()
	printHere(state)
	printElsewhere(state)
	warnEnv()
	return 0
}

func setDir(arg string) int {
	dir, err := claude.SetDir(arg)
	if err != nil {
		ui.Fail(os.Stderr, err)
		return 1
	}
	ui.Done(os.Stdout, "agent directory is now "+shortPath(dir))
	afterSwitch(dir)
	return 0
}

func resetDir() int {
	if err := claude.ResetDir(); err != nil {
		ui.Fail(os.Stderr, err)
		return 1
	}
	dir := claude.Dir()
	ui.Done(os.Stdout, "agent directory is back to the default, "+shortPath(dir))
	afterSwitch(dir)
	return 0
}

// afterSwitch says what the new directory already holds, so a switch is never blind.
func afterSwitch(dir string) {
	state := harness.LoadState()
	printHere(state)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		fmt.Println("  " + ui.Warn.Render("missing  ") +
			ui.Desc.Render("does not exist yet — it is created when a harness is applied"))
	}
	printElsewhere(state)
	warnEnv()
}

// printDir is which directory plst is using, and why.
func printDir() {
	fmt.Println(ui.Note.Render("AGENT DIRECTORY"))
	fmt.Println("  " + ui.Name.Render(shortPath(claude.Dir())))
	if claude.Configured() {
		fmt.Println("  " + ui.Desc.Render("set by claude_dir in "+shortPath(plst.ConfigPath())))
	} else {
		fmt.Println("  " + ui.Desc.Render("the default — claude_dir is not set"))
	}
}

// printHere is the harness applied to the directory plst is using.
func printHere(state harness.State) {
	if a, ok := state.Active(harness.User); ok {
		fmt.Println("  " + ui.Desc.Render("harness  ") + a.Harness)
		return
	}
	fmt.Println("  " + ui.Desc.Render("harness  none applied here"))
}

// printElsewhere lists the other agent directories that have a harness applied.
func printElsewhere(state harness.State) {
	here := harness.User.Resolve()
	first := true
	for _, sc := range state.Order() {
		dir, ok := sc.IsUser()
		if !ok || sc == here {
			continue
		}
		if first {
			fmt.Println()
			fmt.Println(ui.Note.Render("APPLIED IN OTHER DIRECTORIES"))
			first = false
		}
		a, _ := state.Active(sc)
		fmt.Printf("  %s%s\n", ui.Pad(ui.Name.Render(a.Harness), 22), ui.Desc.Render(shortPath(dir)))
	}
}

// warnEnv says so when CLAUDE_CONFIG_DIR would send an agent started from this
// shell somewhere other than where plst is applying harnesses.
func warnEnv() {
	env, ok := claude.EnvOverride()
	if !ok {
		return
	}
	fmt.Println()
	fmt.Println("  " + ui.Warn.Render("! ") + "CLAUDE_CONFIG_DIR is " + shortPath(env) + " in this shell")
	fmt.Println(ui.Desc.Render("    an agent started from here reads that, not " + shortPath(claude.Dir())))
}
