package ui

import (
	"maps"
	"testing"

	"github.com/JacobAtchley/boardwalk/internal/azdo"
	"github.com/JacobAtchley/boardwalk/internal/config"
)

func subjectPR() azdo.PullRequest {
	return azdo.PullRequest{ID: 812, Title: "Retry on 5xx", Repo: "platform", Author: "Dev Example",
		IsDraft: true, Source: "refs/heads/feature/retry", Target: "refs/heads/main"}
}

func TestPullRequestSubject(t *testing.T) {
	c, _ := fixture()
	want := map[string]string{
		"ID": "812", "TITLE": "Retry on 5xx", "URL": c.PullRequestURL("platform", 812),
		"REPO": "platform", "SOURCE_BRANCH": "feature/retry", "TARGET_BRANCH": "main",
		"AUTHOR": "Dev Example", "IS_DRAFT": "true",
	}
	got := pullRequestSubject(c, subjectPR())
	if got.kind != config.KindPullRequest || !maps.Equal(got.env, want) {
		t.Errorf("subject = %+v, want %v", got, want)
	}
}

func TestWorkItemSubject(t *testing.T) {
	c, items := fixture()
	wi := items[0]
	want := map[string]string{
		"ID": "4021", "TITLE": wi.Title, "URL": c.WorkItemURL(4021),
		"TYPE": "User Story", "STATE": "Active",
	}
	got := workItemSubject(c, wi)
	if got.kind != config.KindWorkItem || !maps.Equal(got.env, want) {
		t.Errorf("subject = %+v, want %v", got, want)
	}
}

func TestBuildSubject(t *testing.T) {
	c, _ := fixture()
	b := buildStub()
	b.SourceBranch = "refs/heads/main"
	want := map[string]string{
		"ID": "9001", "TITLE": "platform-ci #20260911.3", "URL": c.BuildURL(9001),
		"NUMBER": "20260911.3", "PIPELINE": "platform-ci", "BRANCH": "main", "RESULT": "succeeded",
	}
	got := buildSubject(c, b)
	if got.kind != config.KindBuild || !maps.Equal(got.env, want) {
		t.Errorf("subject = %+v, want %v", got, want)
	}
}

func TestEveryViewWithASubjectNamesIt(t *testing.T) {
	c, items := fixture()
	pr := subjectPR()

	prs := NewPullRequests(c)
	// NewPullRequests scopes to the working directory's repository when there
	// is one; pin it so the test does not depend on this checkout.
	prs.repoOnly = false
	prs.drafts = draftsAll // subjectPR is a draft, which the list hides by default
	prs.Update(prsMsg{PRs: []azdo.PullRequest{pr}})
	wis := NewWorkItems(c, items, false, false)
	builds := NewBuilds(c)
	builds.Update(buildsMsg{Builds: []azdo.Build{buildStub()}})

	for _, tc := range []struct {
		name string
		view View
		kind string
		id   string
	}{
		{"pr list", prs, config.KindPullRequest, "812"},
		{"pr detail", NewPullRequestDetail(c, pr, nil), config.KindPullRequest, "812"},
		{"pr diff", NewPullRequestDiff(c, pr, nil, filterAll), config.KindPullRequest, "812"},
		{"work items", wis, config.KindWorkItem, "4021"},
		{"item", NewItem(c, items[0], nil), config.KindWorkItem, "4021"},
		{"builds", builds, config.KindBuild, "9001"},
		{"logs", NewLogs(c, buildStub(), nil), config.KindBuild, "9001"},
	} {
		s, ok := tc.view.(subjecter)
		if !ok {
			t.Errorf("%s has no Subject", tc.name)
			continue
		}
		got, ok := s.Subject()
		if !ok || got.kind != tc.kind || got.env["ID"] != tc.id {
			t.Errorf("%s: Subject() = %+v, %v; want %s %s", tc.name, got, ok, tc.kind, tc.id)
		}
	}
}

func TestAnEmptyListHasNoSubjectButKeepsItsKind(t *testing.T) {
	c, _ := fixture()
	for _, tc := range []struct {
		view subjecter
		kind string
	}{
		{NewPullRequests(c), config.KindPullRequest},
		{NewWorkItems(c, nil, false, false), config.KindWorkItem},
		{NewBuilds(c), config.KindBuild},
	} {
		got, ok := tc.view.Subject()
		if ok || got.kind != tc.kind {
			t.Errorf("%T: Subject() = %+v, %v; want kind %s and nothing selected", tc.view, got, ok, tc.kind)
		}
	}
}
