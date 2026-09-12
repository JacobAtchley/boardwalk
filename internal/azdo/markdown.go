package azdo

import (
	"strings"
	"sync"

	md "github.com/JohannesKaufmann/html-to-markdown"
)

// converter is built once. Constructing one parses a rule set, and these
// fields arrive a few hundred at a time.
var (
	converterOnce sync.Once
	converter     *md.Converter
)

// Markdown turns the HTML Azure DevOps stores in its long-text fields into
// markdown a renderer can style.
//
// StripHTML, which this does not replace, flattens the same input to one run of
// prose — fine for a list row, wrong for a description or acceptance criteria,
// where the structure is the content. A criteria list read as a paragraph is
// not much use to anyone.
//
// Text that is not HTML passes through unchanged, because some projects write
// markdown straight into these fields and some write plain sentences.
func Markdown(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}

	// Input with no tags is either plain text or markdown someone typed
	// directly, and conversion can only damage it: the converter escapes
	// markdown syntax it finds in text, so an acceptance criteria already
	// written as markdown came back reading "\*\*bold\*\*".
	if !htmlTag.MatchString(s) {
		return strings.TrimSpace(s)
	}

	converterOnce.Do(func() { converter = md.NewConverter("", true, nil) })

	out, err := converter.ConvertString(s)
	if err != nil || strings.TrimSpace(out) == "" {
		// Showing flattened prose beats showing an empty pane.
		return StripHTML(s)
	}
	return strings.TrimSpace(out)
}
