// boardwalk — a terminal browser for Azure DevOps boards.
//
//	boardwalk            # every open work item in the project
//	boardwalk -mine      # start filtered to items assigned to you
//	boardwalk -all       # include closed/done/resolved/removed
//	boardwalk -dump      # print rows and exit, no TUI
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

func main() {
	var (
		mineOnly    = flag.Bool("mine", false, "start filtered to items assigned to you")
		all         = flag.Bool("all", false, "include closed/done/resolved/removed")
		dump        = flag.Bool("dump", false, "print rows and exit, no TUI")
		timing      = flag.Bool("timing", false, "report fetch duration on stderr")
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("boardwalk", version)
		return
	}

	if err := run(*mineOnly, *all, *dump, *timing); err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}
}

func run(mineOnly, all, dump, timing bool) error {
	org, project := os.Getenv("AZDO_ORG"), os.Getenv("AZDO_PROJECT")
	if org == "" || project == "" {
		return fmt.Errorf("AZDO_ORG and AZDO_PROJECT must be set")
	}

	client, err := azdo.NewClient(org, project)
	if err != nil {
		return err
	}

	start := time.Now()
	items, err := client.WorkItems(all)
	if err != nil {
		return fmt.Errorf("could not fetch work items: %w", err)
	}
	if timing {
		fmt.Fprintf(os.Stderr, "fetched %d work items in %s\n", len(items), time.Since(start).Round(time.Millisecond))
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

	final, err := tea.NewProgram(ui.NewWorkItems(client, items, mineOnly), tea.WithAltScreen()).Run()
	if err != nil {
		return err
	}

	// Anything the view wants the parent shell to run comes back on stdout for
	// the shell wrapper to put on the prompt.
	if m, ok := final.(ui.WorkItems); ok && m.Command != "" {
		fmt.Println(m.Command)
	}
	return nil
}
