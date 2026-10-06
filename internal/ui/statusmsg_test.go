package ui

import (
	"strings"
	"testing"
)

func TestSubjectViewsShowAStatusMsgWithItsColour(t *testing.T) {
	c, items := fixture()
	for _, tc := range []struct {
		name string
		view View
	}{
		{"logs", NewLogs(c, buildStub(), nil)},
		{"pr detail", NewPullRequestDetail(c, subjectPR(), nil)},
		{"item", NewItem(c, items[0], nil)},
	} {
		v, _ := tc.view.Update(StatusMsg{Text: "boom", Err: true})
		if text, isErr := v.Status(); !strings.HasSuffix(text, "boom") || !isErr {
			t.Errorf("%s: after an error, Status() = %q, %v", tc.name, text, isErr)
		}
		v, _ = v.Update(StatusMsg{Text: "fine"})
		if text, isErr := v.Status(); !strings.HasSuffix(text, "fine") || isErr {
			t.Errorf("%s: after a success, Status() = %q, %v", tc.name, text, isErr)
		}
	}
}

func TestAStatusErrorDoesNotClaimTheDiscussionFailed(t *testing.T) {
	// failed means the discussion did not load, and the body says so. A
	// command failing is not that.
	c, items := fixture()
	for _, tc := range []struct {
		name string
		view View
	}{
		{"pr detail", NewPullRequestDetail(c, subjectPR(), nil)},
		{"item", NewItem(c, items[0], nil)},
	} {
		v, _ := tc.view.Update(StatusMsg{Text: "boom", Err: true})
		if body := v.Body(120, 40); strings.Contains(body, "could not load the discussion") {
			t.Errorf("%s: a status error replaced the discussion:\n%s", tc.name, body)
		}
	}
}
