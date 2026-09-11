package azdo

import (
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestStripHTML(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"<div>A &amp; B</div><br>&nbsp;C", "A & B C"},
		{"plain text", "plain text"},
		{"", ""},
		{"<p>a</p><p>b</p>", "a b"},
		{"&lt;script&gt;", "<script>"},
	} {
		if got := StripHTML(tc.in); got != tc.want {
			t.Errorf("StripHTML(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestChunkCoversEveryIDExactlyOnce(t *testing.T) {
	ids := make([]int, 450)
	for i := range ids {
		ids[i] = i
	}

	chunks := chunk(ids, 200)
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}

	var flat []int
	for _, c := range chunks {
		if len(c) > 200 {
			t.Errorf("chunk of %d exceeds the batch limit", len(c))
		}
		flat = append(flat, c...)
	}
	if !reflect.DeepEqual(flat, ids) {
		t.Error("chunking lost or reordered ids")
	}
}

func TestChunkHandlesEmptyInput(t *testing.T) {
	if got := chunk(nil, 200); got != nil {
		t.Errorf("chunk(nil) = %v, want nil", got)
	}
}

func TestMineOf(t *testing.T) {
	items := []WorkItem{
		{ID: 1, AssignedKey: "dev@acme.test"},
		{ID: 2, AssignedKey: "other@acme.test"},
		{ID: 3, AssignedKey: ""},
		{ID: 4, AssignedKey: "dev@acme.test"},
	}

	mine := MineOf(items, "dev@acme.test")
	if len(mine) != 2 || mine[0].ID != 1 || mine[1].ID != 4 {
		t.Errorf("MineOf returned %+v, want items 1 and 4", mine)
	}

	// An unknown identity must match nothing, rather than matching the
	// unassigned items whose key is also empty.
	if got := MineOf(items, ""); got != nil {
		t.Errorf("MineOf with no identity = %v, want nil", got)
	}
}

func TestBatchFieldsIncludeAcceptanceCriteria(t *testing.T) {
	var body string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		body = string(buf)
		w.Write([]byte(`{"value":[{"id":4021,"fields":{
			"System.Title":"Retry webhooks",
			"Microsoft.VSTS.Common.AcceptanceCriteria":"<ul><li>Retries three times</li></ul>"
		}}]}`))
	})

	items, err := c.batch([]int{4021})
	if err != nil {
		t.Fatalf("batch returned %v", err)
	}
	if !strings.Contains(body, "Microsoft.VSTS.Common.AcceptanceCriteria") {
		t.Error("the batch request did not ask for acceptance criteria")
	}
	if items[0].AcceptanceCriteria != "Retries three times" {
		t.Errorf("acceptance criteria = %q, want the HTML stripped", items[0].AcceptanceCriteria)
	}
}
