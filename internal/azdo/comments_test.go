package azdo

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCommentsParsesAndStripsHTML(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("api-version"); got != CommentsAPIVersion {
			t.Errorf("api-version = %q, want %q", got, CommentsAPIVersion)
		}
		w.Write([]byte(`{"count":2,"comments":[
			{"id":1,"text":"<div>Looks good &amp; shipped</div>","createdBy":{"displayName":"Dev Example"},"createdDate":"2026-09-10T09:00:00Z"},
			{"id":2,"text":"<p>Second</p>","createdBy":{"displayName":"Other Dev"},"createdDate":"2026-09-11T10:30:00Z"}
		]}`))
	})

	comments, err := c.Comments(4021)
	if err != nil {
		t.Fatalf("Comments returned %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("got %d comments, want 2", len(comments))
	}
	if comments[0].Text != "Looks good & shipped" {
		t.Errorf("text = %q, want the HTML stripped", comments[0].Text)
	}
	if comments[0].Author != "Dev Example" {
		t.Errorf("author = %q", comments[0].Author)
	}
	if comments[0].Created.IsZero() {
		t.Error("createdDate did not parse")
	}
}

func TestCommentsOnAnItemWithNone(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"count":0,"comments":[]}`))
	})

	comments, err := c.Comments(4021)
	if err != nil {
		t.Fatalf("Comments returned %v", err)
	}
	if len(comments) != 0 {
		t.Errorf("got %d comments, want none", len(comments))
	}
}

func TestCommentsSurfacesAnError(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"access denied"}`))
	})

	if _, err := c.Comments(4021); err == nil || !strings.Contains(err.Error(), "access denied") {
		t.Errorf("error = %v, want the server message", err)
	}
}

func TestAddCommentPostsTheTextAndUsesTheCommentsAPIVersion(t *testing.T) {
	var method, path, body string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		if got := r.URL.Query().Get("api-version"); got != CommentsAPIVersion {
			t.Errorf("api-version = %q, want %q", got, CommentsAPIVersion)
		}
		buf, _ := io.ReadAll(r.Body)
		body = string(buf)
		w.Write([]byte(`{"text":"Looks good","createdDate":"2026-09-12T10:00:00Z",
		 "createdBy":{"displayName":"Dev Example"}}`))
	})

	comment, err := c.AddComment(4021, "Looks good")
	if err != nil {
		t.Fatalf("AddComment returned %v", err)
	}
	if method != http.MethodPost {
		t.Errorf("method = %s, want POST", method)
	}
	if path != "/acme/Platform/_apis/wit/workItems/4021/comments" {
		t.Errorf("path = %q", path)
	}
	if body != `{"text":"Looks good"}` {
		t.Errorf("body = %q", body)
	}
	if comment.Author != "Dev Example" || comment.Text != "Looks good" {
		t.Errorf("comment = %+v, want the server's created comment", comment)
	}
	if comment.Created.IsZero() {
		t.Error("createdDate did not parse")
	}
}

func TestAddCommentReportsAServerRefusal(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"TF401232: work item does not exist"}`))
	})

	_, err := c.AddComment(4021, "hi")
	if err == nil || !strings.Contains(err.Error(), "TF401232") {
		t.Errorf("error = %v, want the server message", err)
	}
}
