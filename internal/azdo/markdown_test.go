package azdo

import (
	"strings"
	"testing"
)

func TestMarkdownKeepsStructureHTMLCarries(t *testing.T) {
	// StripHTML flattens a description to one run of prose. These fields are
	// written in Azure DevOps's rich text editor, so the structure is the
	// content: an acceptance criteria list read as a paragraph is unusable.
	for _, tc := range []struct {
		name, in string
		want     []string
	}{
		{
			name: "a bulleted list stays a list",
			in:   "<ul><li>Retries three times</li><li>Backs off exponentially</li></ul>",
			want: []string{"- Retries three times", "- Backs off exponentially"},
		},
		{
			name: "headings survive",
			in:   "<h2>Context</h2><p>Deliveries fail.</p>",
			want: []string{"## Context", "Deliveries fail."},
		},
		{
			name: "emphasis survives",
			in:   "<p>This is <strong>important</strong> and <em>urgent</em>.</p>",
			want: []string{"**important**", "_urgent_"},
		},
		{
			name: "links keep their target",
			in:   `<p>See <a href="https://example.test/x">the spec</a>.</p>`,
			want: []string{"[the spec](https://example.test/x)"},
		},
		{
			name: "code blocks survive",
			in:   "<pre><code>retry(3)</code></pre>",
			want: []string{"retry(3)"},
		},
	} {
		got := Markdown(tc.in)
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Errorf("%s: Markdown(%q) = %q, missing %q", tc.name, tc.in, got, want)
			}
		}
	}
}

func TestMarkdownOnTextThatIsNotHTML(t *testing.T) {
	// Some fields come back as plain text, and some projects write markdown
	// into them directly. Neither may be mangled.
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"just a sentence", "just a sentence"},
		{"already **bold**", "already **bold**"},
	} {
		if got := Markdown(tc.in); got != tc.want {
			t.Errorf("Markdown(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMarkdownDecodesEntities(t *testing.T) {
	if got := Markdown("<p>A &amp; B &lt; C</p>"); !strings.Contains(got, "A & B < C") {
		t.Errorf("Markdown = %q, want the entities decoded", got)
	}
}

func TestMarkdownFallsBackToStrippedText(t *testing.T) {
	// The converter can refuse input; a pane showing nothing at all would be
	// worse than a pane showing flattened prose.
	in := "<p>unclosed <b>markup"
	got := Markdown(in)
	if got == "" {
		t.Error("Markdown returned nothing for markup it could not convert cleanly")
	}
	if !strings.Contains(got, "unclosed") {
		t.Errorf("Markdown = %q, want the text preserved", got)
	}
}
