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
	"os"
	"time"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/JacobAtchley/boardwalk/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

// version is overridden at build time: -ldflags "-X main.version=$(git describe)"
var version = "dev"

// subcommands are the views boardwalk can open directly, skipping the menu.
var subcommands = map[string]bool{"items": true, "prs": true, "builds": true}

func main() {
	start, rest := parseArgs(os.Args[1:])

	fs := flag.NewFlagSet("boardwalk", flag.ExitOnError)
	var (
		mineOnly    = fs.Bool("mine", false, "start filtered to items assigned to you")
		all         = fs.Bool("all", false, "include closed/done/resolved/removed")
		dump        = fs.Bool("dump", false, "print work item rows and exit, no TUI")
		timing      = fs.Bool("timing", false, "report fetch duration on stderr")
		showVersion = fs.Bool("version", false, "print version and exit")
	)
	fs.Parse(rest)

	if *showVersion {
		fmt.Println("boardwalk", version)
		return
	}

	if err := run(startFor(start, *mineOnly, *all), *mineOnly, *all, *dump, *timing); err != nil {
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

func run(start string, mineOnly, all, dump, timing bool) error {
	org, project := os.Getenv("AZDO_ORG"), os.Getenv("AZDO_PROJECT")
	if org == "" || project == "" {
		return fmt.Errorf("AZDO_ORG and AZDO_PROJECT must be set")
	}

	client, err := azdo.NewClient(org, project)
	if err != nil {
		return err
	}

	// Work items are the one view whose data is fetched before the program
	// starts: the whole project comes back in one pass, and -dump needs it
	// without a TUI at all. The other views fetch on entry.
	var items []azdo.WorkItem
	if dump || start == "items" || start == "" {
		fetchStart := time.Now()
		if items, err = client.WorkItems(all); err != nil {
			return fmt.Errorf("could not fetch work items: %w", err)
		}
		if timing {
			fmt.Fprintf(os.Stderr, "fetched %d work items in %s\n",
				len(items), time.Since(fetchStart).Round(time.Millisecond))
		}
	}

	if dump {
		if mineOnly {
			items = azdo.MineOf(items, client.Me)
		}
		for _, wi := range items {
			fmt.Printf("%-7d %-18s %-16s %-20s %s\n", wi.ID, "["+wi.Type+"]", wi.State, wi.Assigned, wi.Title)
		}
		return nil
	}

	root := ui.NewRoot(client, items, mineOnly, start)
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
