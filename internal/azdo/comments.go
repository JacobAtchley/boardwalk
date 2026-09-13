package azdo

import (
	"fmt"
	"net/url"
	"time"
)

// CommentsAPIVersion overrides the pinned version for this one endpoint. Work
// item comments have never left preview, and 7.1 alone returns a 404.
const CommentsAPIVersion = "7.1-preview.3"

// Comment is one entry in a work item's discussion.
type Comment struct {
	Author  string
	Created time.Time
	Text    string
}

// Comments returns a work item's discussion, oldest first, which is the order
// the API answers in and the order a conversation reads in.
func (c *Client) Comments(id int) ([]Comment, error) {
	var resp struct {
		Comments []struct {
			Text      string    `json:"text"`
			Created   time.Time `json:"createdDate"`
			CreatedBy struct {
				DisplayName string `json:"displayName"`
			} `json:"createdBy"`
		} `json:"comments"`
	}

	endpoint := fmt.Sprintf("%s/%s/%s/_apis/wit/workItems/%d/comments?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), id, CommentsAPIVersion)
	if err := c.get(endpoint, &resp); err != nil {
		return nil, err
	}

	comments := make([]Comment, 0, len(resp.Comments))
	for _, v := range resp.Comments {
		comments = append(comments, Comment{
			Author:  v.CreatedBy.DisplayName,
			Created: v.Created,
			Text:    Markdown(v.Text),
		})
	}
	return comments, nil
}

// AddComment posts a new comment to a work item's discussion. It is built
// against CommentsAPIVersion rather than the pinned APIVersion, for the same
// reason the read above is: this endpoint has never left preview, and the
// plain version already 404s on the GET side of it.
func (c *Client) AddComment(id int, text string) (Comment, error) {
	body := struct {
		Text string `json:"text"`
	}{Text: text}

	var resp struct {
		Text      string    `json:"text"`
		Created   time.Time `json:"createdDate"`
		CreatedBy struct {
			DisplayName string `json:"displayName"`
		} `json:"createdBy"`
	}

	endpoint := fmt.Sprintf("%s/%s/%s/_apis/wit/workItems/%d/comments?api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project), id, CommentsAPIVersion)
	if err := c.post(endpoint, body, &resp); err != nil {
		return Comment{}, err
	}
	return Comment{
		Author:  resp.CreatedBy.DisplayName,
		Created: resp.Created,
		Text:    Markdown(resp.Text),
	}, nil
}
