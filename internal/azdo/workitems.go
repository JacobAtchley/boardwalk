package azdo

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

// workitemsbatch refuses more than 200 ids per call.
const batchSize = 200

// batchConcurrency caps in-flight batch requests. Eight is enough to saturate
// the fetch without inviting throttling.
const batchConcurrency = 8

type WorkItem struct {
	ID          int
	Title       string
	Type        string
	State       string
	Assigned    string
	AssignedKey string // uniqueName, lowercased, compared against Client.Me
	Tags        string
	Iteration   string
	Description string
}

// MineOf returns the subset assigned to the signed-in user.
func MineOf(items []WorkItem, me string) []WorkItem {
	if me == "" {
		return nil
	}
	var mine []WorkItem
	for _, wi := range items {
		if wi.AssignedKey == me {
			mine = append(mine, wi)
		}
	}
	return mine
}

// WorkItems returns every work item in the project, newest id first.
//
// WIQL only ever returns ids, so the fields come from a second pass against
// workitemsbatch. That pass is the slow part and is what gets parallelised.
func (c *Client) WorkItems(includeClosed bool) ([]WorkItem, error) {
	ids, err := c.workItemIDs(includeClosed)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return c.workItemFields(ids)
}

func (c *Client) workItemIDs(includeClosed bool) ([]int, error) {
	stateClause := ""
	if !includeClosed {
		stateClause = " AND [System.State] NOT IN ('Closed','Done','Resolved','Removed')"
	}

	// The project name is interpolated into WIQL rather than parameterised —
	// the API takes no bind parameters. A single quote would break the query,
	// so it is doubled, which is WIQL's own escape.
	project := strings.ReplaceAll(c.Project, "'", "''")
	query := fmt.Sprintf(
		"SELECT [System.Id] FROM WorkItems WHERE [System.TeamProject] = '%s'%s ORDER BY [System.Id] DESC",
		project, stateClause)

	var resp struct {
		WorkItems []struct {
			ID int `json:"id"`
		} `json:"workItems"`
	}
	endpoint := fmt.Sprintf("%s/%s/%s/_apis/wit/wiql?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), APIVersion)
	if err := c.post(endpoint, map[string]string{"query": query}, &resp); err != nil {
		return nil, err
	}

	ids := make([]int, 0, len(resp.WorkItems))
	for _, wi := range resp.WorkItems {
		ids = append(ids, wi.ID)
	}
	return ids, nil
}

func (c *Client) workItemFields(ids []int) ([]WorkItem, error) {
	chunks := chunk(ids, batchSize)
	results := make([][]WorkItem, len(chunks))
	errs := make([]error, len(chunks))

	var wg sync.WaitGroup
	sem := make(chan struct{}, batchConcurrency)
	for i, ch := range chunks {
		wg.Add(1)
		go func(i int, ids []int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i], errs[i] = c.batch(ids)
		}(i, ch)
	}
	wg.Wait()

	// workitemsbatch answers in ascending id order no matter what order the ids
	// arrived in, so the query's ORDER BY has to be reapplied here.
	byID := make(map[int]WorkItem, len(ids))
	for i := range chunks {
		if errs[i] != nil {
			return nil, errs[i]
		}
		for _, wi := range results[i] {
			byID[wi.ID] = wi
		}
	}

	items := make([]WorkItem, 0, len(ids))
	for _, id := range ids {
		if wi, ok := byID[id]; ok {
			items = append(items, wi)
		}
	}
	return items, nil
}

func (c *Client) batch(ids []int) ([]WorkItem, error) {
	body := map[string]any{
		"ids": ids,
		"fields": []string{
			"System.Id", "System.Title", "System.WorkItemType", "System.State",
			"System.AssignedTo", "System.Tags", "System.IterationPath", "System.Description",
		},
	}

	var resp struct {
		Value []struct {
			ID     int `json:"id"`
			Fields struct {
				Title       string `json:"System.Title"`
				Type        string `json:"System.WorkItemType"`
				State       string `json:"System.State"`
				Tags        string `json:"System.Tags"`
				Iteration   string `json:"System.IterationPath"`
				Description string `json:"System.Description"`
				AssignedTo  *struct {
					DisplayName string `json:"displayName"`
					UniqueName  string `json:"uniqueName"`
				} `json:"System.AssignedTo"`
			} `json:"fields"`
		} `json:"value"`
	}

	endpoint := fmt.Sprintf("%s/%s/_apis/wit/workitemsbatch?api-version=%s",
		c.root(), url.PathEscape(c.Org), APIVersion)
	if err := c.post(endpoint, body, &resp); err != nil {
		return nil, err
	}

	items := make([]WorkItem, 0, len(resp.Value))
	for _, v := range resp.Value {
		item := WorkItem{
			ID:          v.ID,
			Title:       v.Fields.Title,
			Type:        v.Fields.Type,
			State:       v.Fields.State,
			Tags:        v.Fields.Tags,
			Iteration:   v.Fields.Iteration,
			Description: StripHTML(v.Fields.Description),
			Assigned:    "(unassigned)",
		}
		if v.Fields.AssignedTo != nil {
			item.Assigned = v.Fields.AssignedTo.DisplayName
			item.AssignedKey = strings.ToLower(v.Fields.AssignedTo.UniqueName)
		}
		items = append(items, item)
	}
	return items, nil
}

// WorkItemURL is the browser URL for a work item.
func (c *Client) WorkItemURL(id int) string {
	return fmt.Sprintf("https://dev.azure.com/%s/%s/_workitems/edit/%d",
		url.PathEscape(c.Org), url.PathEscape(c.Project), id)
}

func chunk(ids []int, size int) [][]int {
	var out [][]int
	for i := 0; i < len(ids); i += size {
		out = append(out, ids[i:min(i+size, len(ids))])
	}
	return out
}

var (
	htmlTag    = regexp.MustCompile(`<[^>]*>`)
	runOfSpace = regexp.MustCompile(`[ \t]+`)
	entities   = strings.NewReplacer(
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'",
	)
)

// StripHTML flattens the HTML that Azure DevOps stores in long-text fields into
// something a terminal pane can show.
func StripHTML(s string) string {
	s = htmlTag.ReplaceAllString(s, " ")
	s = entities.Replace(s)
	s = runOfSpace.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
