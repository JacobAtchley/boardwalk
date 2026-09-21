package azdo

import (
	"fmt"
	"net/url"
)

// buildPolicyTypeID is Azure DevOps's identifier for the build validation
// policy. The evaluations endpoint returns every policy on a pull request —
// reviewer counts, linked work items, comment resolution — and this is the one
// that runs a pipeline, so it is the only one that can name a build.
const buildPolicyTypeID = "0609b952-1397-4640-95ec-e00a01b2c241"

// evaluationsAPIVersion is APIVersion's preview twin for this endpoint, the
// same arrangement connectionData needs. Policy evaluations are still under
// preview, and asking for a plain version is refused outright: "The requested
// version \"7.1\" of the resource is under preview. The -preview flag must be
// supplied in the api-version for such requests." The .1 is the revision the
// shape read below belongs to; a bare -preview is accepted too, but pinning
// the revision is the point of pinning the version at all.
const evaluationsAPIVersion = APIVersion + "-preview.1"

// unnamedPolicy is what a build validation with no display name is called. A
// policy is named when it is configured and most are, but an unnamed one still
// gates the pull request and still has a run worth opening.
const unnamedPolicy = "build validation"

// GateBuild is one build validation policy on a pull request, and the run it
// produced. Status is the policy's own verdict — queued, running, approved,
// rejected — which is not the same as the build's: a rejected gate whose build
// succeeded means the policy was evaluated against an older iteration.
type GateBuild struct {
	BuildID int
	Policy  string
	Status  string
}

// PullRequestGateBuilds lists the runs the build validation policies on pr
// have produced, newest evaluation first as the endpoint returns them.
//
// A policy with no run yet is left out rather than returned with a zero id:
// the caller's whole purpose is opening the build, and a row that cannot be
// opened is worse than a shorter list that says so.
func (c *Client) PullRequestGateBuilds(pr PullRequest) ([]GateBuild, error) {
	if pr.ProjectID == "" {
		return nil, fmt.Errorf("!%d does not say which project its repository belongs to, so its build gates cannot be looked up", pr.ID)
	}

	var resp struct {
		Value []struct {
			Configuration struct {
				Type struct {
					ID string `json:"id"`
				} `json:"type"`
				Settings struct {
					DisplayName string `json:"displayName"`
				} `json:"settings"`
			} `json:"configuration"`
			Status  string `json:"status"`
			Context struct {
				BuildID int `json:"buildId"`
			} `json:"context"`
		} `json:"value"`
	}

	artifact := fmt.Sprintf("vstfs:///CodeReview/CodeReviewId/%s/%d", pr.ProjectID, pr.ID)
	endpoint := fmt.Sprintf("%s/%s/%s/_apis/policy/evaluations?artifactId=%s&api-version=%s",
		c.root(), url.PathEscape(c.Org), url.PathEscape(c.Project),
		url.QueryEscape(artifact), evaluationsAPIVersion)
	if err := c.get(endpoint, &resp); err != nil {
		return nil, err
	}

	var gates []GateBuild
	for _, v := range resp.Value {
		if v.Configuration.Type.ID != buildPolicyTypeID || v.Context.BuildID == 0 {
			continue
		}
		name := v.Configuration.Settings.DisplayName
		if name == "" {
			name = unnamedPolicy
		}
		gates = append(gates, GateBuild{BuildID: v.Context.BuildID, Policy: name, Status: v.Status})
	}
	return gates, nil
}
