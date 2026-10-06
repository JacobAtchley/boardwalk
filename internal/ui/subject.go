package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/JacobAtchley/boardwalk/internal/config"
)

// subject is the one thing a screen is showing that a custom command can act
// on: its kind, and the variables that describe it, named without their
// BOARDWALK_ prefix. Root adds the prefix and the variables every kind shares.
type subject struct {
	kind string
	env  map[string]string
}

// subjecter is a view a custom command can run on. kind is set even when ok
// is false — an empty list is still a pull request screen, and Root has to
// know which commands to say "nothing selected" for.
type subjecter interface {
	Subject() (s subject, ok bool)
}

// refBranch strips refs/heads/. shortRef is not used: it answers "-" for an
// empty ref, which reads well in a column and badly in a script.
func refBranch(ref string) string { return strings.TrimPrefix(ref, "refs/heads/") }

func pullRequestSubject(c *azdo.Client, pr azdo.PullRequest) subject {
	return subject{kind: config.KindPullRequest, env: map[string]string{
		"ID":            strconv.Itoa(pr.ID),
		"TITLE":         pr.Title,
		"URL":           c.PullRequestURL(pr.Repo, pr.ID),
		"REPO":          pr.Repo,
		"SOURCE_BRANCH": refBranch(pr.Source),
		"TARGET_BRANCH": refBranch(pr.Target),
		"AUTHOR":        pr.Author,
		"IS_DRAFT":      strconv.FormatBool(pr.IsDraft),
	}}
}

func workItemSubject(c *azdo.Client, wi azdo.WorkItem) subject {
	return subject{kind: config.KindWorkItem, env: map[string]string{
		"ID":    strconv.Itoa(wi.ID),
		"TITLE": wi.Title,
		"URL":   c.WorkItemURL(wi.ID),
		"TYPE":  wi.Type,
		"STATE": wi.State,
	}}
}

func buildSubject(c *azdo.Client, b azdo.Build) subject {
	return subject{kind: config.KindBuild, env: map[string]string{
		"ID":       strconv.Itoa(b.ID),
		"TITLE":    fmt.Sprintf("%s #%s", b.Pipeline, b.Number),
		"URL":      c.BuildURL(b.ID),
		"NUMBER":   b.Number,
		"PIPELINE": b.Pipeline,
		"BRANCH":   refBranch(b.SourceBranch),
		"RESULT":   b.Status.String(),
	}}
}
