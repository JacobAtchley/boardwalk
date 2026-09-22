package ui

import "github.com/JacobAtchley/boardwalk/internal/azdo"

// threadFilter is which half of a pull request's discussion is on screen. A
// review with a lot of feedback on it is the case this exists for: the
// unresolved threads are what is still owed, and reading them means scrolling
// past every settled one to find out.
//
// It cycles rather than toggles because both halves are worth looking at on
// their own — what is left to do, and what was already answered — and a single
// toggle can only offer one of them.
type threadFilter int

const (
	filterAll threadFilter = iota
	filterUnresolved
	filterResolved
)

// next is the mode one press of the filter key moves to.
func (f threadFilter) next() threadFilter {
	if f == filterResolved {
		return filterAll
	}
	return f + 1
}

// keep reports whether a thread is shown under this filter.
//
// Unresolved is the negation of Thread.Resolved rather than
// azdo.unresolvedStatus, so a thread whose status Azure DevOps left unset —
// counted in neither of Summarize's tallies — reads as not yet settled and
// stays on screen. Discussion that exists and is hidden with nothing saying so
// is the one outcome this feature must not produce.
func (f threadFilter) keep(t azdo.Thread) bool {
	switch f {
	case filterUnresolved:
		return !t.Resolved
	case filterResolved:
		return t.Resolved
	}
	return true
}

// apply is keep over a whole discussion, in the order it was given in.
func (f threadFilter) apply(threads []azdo.Thread) []azdo.Thread {
	if f == filterAll {
		return threads
	}
	var out []azdo.Thread
	for _, t := range threads {
		if f.keep(t) {
			out = append(out, t)
		}
	}
	return out
}

// noun names what this filter shows, for the sentence that says a filter has
// hidden everything. It is empty for filterAll, which hides nothing.
func (f threadFilter) noun() string {
	switch f {
	case filterUnresolved:
		return "unresolved"
	case filterResolved:
		return "resolved"
	}
	return ""
}

// label is what the heading calls this mode.
func (f threadFilter) label() string {
	if f == filterAll {
		return "all"
	}
	return f.noun() + " only"
}
