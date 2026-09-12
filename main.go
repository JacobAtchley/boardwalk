// boardwalk — a terminal browser for Azure DevOps.
//
//	boardwalk            # the menu: work items, pull requests, builds
//	boardwalk items      # straight to work items
//	boardwalk prs        # straight to pull requests
//	boardwalk builds     # straight to pipeline builds
//	boardwalk -mine      # work items assigned to you
//	boardwalk -all       # include closed/done/resolved/removed
//	boardwalk -dump      # print work item rows and exit, no TUI
//
// Reads AZDO_ORG and AZDO_PROJECT, and borrows the machine's existing
// `az login` session for its token.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/JacobAtchley/boardwalk/internal/config"
	"github.com/JacobAtchley/boardwalk/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

// version is overridden at build time: -ldflags "-X main.version=$(git describe)"
var version = "dev"

// subcommands are the views boardwalk can open directly, skipping the menu.
var subcommands = map[string]bool{"items": true, "prs": true, "builds": true}

// flags is every command-line option boardwalk understands, plus the
// starting view once -mine/-all have been folded in by startFor.
type flags struct {
	start                       string
	mineOnly, all, dump, timing bool
	showVersion                 bool
}

func main() {
	f, err := parseCommandLine(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}

	if f.showVersion {
		fmt.Println("boardwalk", version)
		return
	}

	if err := run(f.start, f.mineOnly, f.all, f.dump, f.timing); err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}
}

// parseArgs peels a leading subcommand off the arguments, leaving the rest for
// the flag package. Writing it by hand rather than reaching for a CLI library
// keeps the dependency list where it is.
func parseArgs(args []string) (start string, rest []string) {
	if len(args) > 0 && subcommands[args[0]] {
		return args[0], args[1:]
	}
	return "", args
}

// startFor decides which view to open. -mine and -all describe work items, so
// either implies that view when no subcommand was given.
func startFor(start string, mineOnly, all bool) string {
	if start != "" {
		return start
	}
	if mineOnly || all {
		return "items"
	}
	return ""
}

// parseCommandLine runs the whole argument pipeline — peel off a subcommand,
// then parse flags — and reports a leftover positional argument as an error
// instead of letting it vanish. The standard flag package stops at the first
// non-flag word without complaint, so a subcommand typed after a flag (e.g.
// "boardwalk -mine prs") or a misspelled subcommand (e.g. "boardwalk builter")
// used to be silently discarded, leaving fs.Args() non-empty with nothing
// reading it. It is split out of main so the pipeline can be exercised by a
// test without invoking main or the built binary, and it makes no network
// call — only flag parsing.
func parseCommandLine(args []string) (flags, error) {
	start, rest := parseArgs(args)

	fs := flag.NewFlagSet("boardwalk", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // main reports the error itself
	var f flags
	fs.BoolVar(&f.mineOnly, "mine", false, "start filtered to items assigned to you")
	fs.BoolVar(&f.all, "all", false, "include closed/done/resolved/removed")
	fs.BoolVar(&f.dump, "dump", false, "print work item rows and exit, no TUI")
	fs.BoolVar(&f.timing, "timing", false, "report the -dump fetch duration on stderr")
	fs.BoolVar(&f.showVersion, "version", false, "print version and exit")
	if err := fs.Parse(rest); err != nil {
		return flags{}, err
	}

	if leftover := fs.Args(); len(leftover) > 0 {
		return flags{}, fmt.Errorf(
			"unexpected argument %q — subcommands come first, e.g. boardwalk %s\n  valid subcommands: items, prs, builds",
			leftover[0], leftover[0])
	}

	f.start = startFor(start, f.mineOnly, f.all)
	return f, nil
}

func run(start string, mineOnly, all, dump, timing bool) error {
	org, project := os.Getenv("AZDO_ORG"), os.Getenv("AZDO_PROJECT")
	if org == "" || project == "" {
		return fmt.Errorf("AZDO_ORG and AZDO_PROJECT must be set")
	}

	client, err := azdo.NewClient(org, project)
	if err != nil {
		return err
	}

	// -dump is the one path that still fetches before anything renders: it
	// prints rows and exits without a TUI, so there is no view to fetch on
	// entry. Every view inside the TUI fetches itself, which is what lets the
	// menu paint immediately and what keeps a failed fetch on the status line
	// instead of exiting the program.
	if dump {
		fetchStart := time.Now()
		items, err := client.WorkItems(all)
		if err != nil {
			return fmt.Errorf("could not fetch work items: %w", err)
		}
		if timing {
			fmt.Fprintf(os.Stderr, "fetched %d work items in %s\n",
				len(items), time.Since(fetchStart).Round(time.Millisecond))
		}

		if mineOnly {
			items = azdo.MineOf(items, client.Me)
		}
		for _, wi := range items {
			fmt.Printf("%-7d %-18s %-16s %-20s %s\n", wi.ID, "["+wi.Type+"]", wi.State, wi.Assigned, wi.Title)
		}
		return nil
	}

	// The config is optional and only ever adds to what boardwalk knows, so a
	// malformed one is worth reporting but not worth refusing to start over.
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
	}

	root := ui.NewRoot(client, mineOnly, all, cfg.ReviewGroups, start)
	final, err := tea.NewProgram(root, tea.WithAltScreen()).Run()
	if err != nil {
		return err
	}

	// Anything the view wants the parent shell to run comes back on stdout for
	// the shell wrapper to put on the prompt.
	if r, ok := final.(*ui.Root); ok && r.ShellCommand() != "" {
		fmt.Println(r.ShellCommand())
	}
	return nil
}
