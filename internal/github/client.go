package github

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
)

type PRStatus struct {
	NodeID           string `json:"id"`
	Number           int    `json:"number"`
	Title            string `json:"title"`
	State            string `json:"state"`
	IsDraft          bool   `json:"isDraft"`
	ReviewDecision   string `json:"reviewDecision"`
	MergeStateStatus string `json:"mergeStateStatus"`
	Mergeable        string `json:"mergeable"`
}

type PollResult struct {
	PR              PRStatus
	ChecksState     string // SUCCESS, FAILURE, PENDING, UNKNOWN
	PassedChecks    int
	FailedChecks    int
	RunningChecks   int
	ApprovedReviews int
	ChangesReviews  int
	PendingReviews  int
}

func GetPRStatus(repo string, prNumber int) (*PollResult, error) {
	client, err := api.DefaultGraphQLClient()
	if err != nil {
		return nil, err
	}

	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Repository struct {
			PullRequest struct {
				ID               string `json:"id"`
				Number           int    `json:"number"`
				Title            string `json:"title"`
				State            string `json:"state"`
				IsDraft          bool   `json:"isDraft"`
				ReviewDecision   string `json:"reviewDecision"`
				MergeStateStatus string `json:"mergeStateStatus"`
				Mergeable        string `json:"mergeable"`
				LatestReviews    struct {
					Nodes []struct {
						State string `json:"state"`
					} `json:"nodes"`
				} `json:"latestReviews"`
				ReviewRequests struct {
					TotalCount int `json:"totalCount"`
				} `json:"reviewRequests"`
				Commits struct {
					Nodes []struct {
						Commit struct {
							StatusCheckRollup struct {
								State    string `json:"state"`
								Contexts struct {
									Nodes []struct {
										Typename   string `json:"__typename"`
										Conclusion string `json:"conclusion"`
										Status     string `json:"status"`
										State      string `json:"state"`
									} `json:"nodes"`
								} `json:"contexts"`
							} `json:"statusCheckRollup"`
						} `json:"commit"`
					} `json:"nodes"`
				} `json:"commits"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}

	err = client.Do(`
		query($owner: String!, $name: String!, $number: Int!) {
			repository(owner: $owner, name: $name) {
				pullRequest(number: $number) {
					id number title state isDraft reviewDecision mergeStateStatus mergeable
					latestReviews(first: 50) {
						nodes { state }
					}
					reviewRequests(first: 50) { totalCount }
					commits(last: 1) {
						nodes {
							commit {
								statusCheckRollup {
									state
									contexts(first: 50) {
										nodes {
											__typename
											... on CheckRun { conclusion status }
											... on StatusContext { state }
										}
									}
								}
							}
						}
					}
				}
			}
		}
	`, map[string]interface{}{
		"owner":  owner,
		"name":   name,
		"number": prNumber,
	}, &resp)
	if err != nil {
		return nil, err
	}

	pr := resp.Repository.PullRequest
	result := &PollResult{
		PR: PRStatus{
			NodeID:           pr.ID,
			Number:           pr.Number,
			Title:            pr.Title,
			State:            pr.State,
			IsDraft:          pr.IsDraft,
			ReviewDecision:   pr.ReviewDecision,
			MergeStateStatus: pr.MergeStateStatus,
			Mergeable:        pr.Mergeable,
		},
		ChecksState:    "UNKNOWN",
		PendingReviews: pr.ReviewRequests.TotalCount,
	}

	for _, rv := range pr.LatestReviews.Nodes {
		switch rv.State {
		case "APPROVED":
			result.ApprovedReviews++
		case "CHANGES_REQUESTED":
			result.ChangesReviews++
		}
	}

	if len(pr.Commits.Nodes) > 0 {
		rollup := pr.Commits.Nodes[0].Commit.StatusCheckRollup
		result.ChecksState = rollup.State
		for _, node := range rollup.Contexts.Nodes {
			switch node.Typename {
			case "CheckRun":
				switch {
				case node.Status != "COMPLETED":
					result.RunningChecks++
				case node.Conclusion == "SUCCESS" || node.Conclusion == "NEUTRAL" || node.Conclusion == "SKIPPED":
					result.PassedChecks++
				default:
					result.FailedChecks++
				}
			case "StatusContext":
				switch node.State {
				case "SUCCESS":
					result.PassedChecks++
				case "PENDING", "EXPECTED":
					result.RunningChecks++
				default:
					result.FailedChecks++
				}
			}
		}
	}

	return result, nil
}

func (r *PollResult) IsReady() bool {
	if r.PR.State != "OPEN" {
		return false
	}
	if r.PR.IsDraft {
		return false
	}
	if r.PR.ReviewDecision == "CHANGES_REQUESTED" || r.PR.ReviewDecision == "REVIEW_REQUIRED" {
		return false
	}
	if r.PR.MergeStateStatus != "CLEAN" {
		return false
	}
	if r.ChecksState != "SUCCESS" && r.ChecksState != "UNKNOWN" {
		return false
	}
	return true
}

func MergePR(nodeID string) error {
	client, err := api.DefaultGraphQLClient()
	if err != nil {
		return err
	}
	var resp json.RawMessage
	return client.Do(`
		mutation($id: ID!) {
			mergePullRequest(input: { pullRequestId: $id, mergeMethod: MERGE }) {
				pullRequest { merged }
			}
		}
	`, map[string]interface{}{"id": nodeID}, &resp)
}

func CurrentRepo() (string, error) {
	repo, err := repository.Current()
	if err != nil {
		return "", fmt.Errorf("could not determine repo: use --repo owner/name")
	}
	return fmt.Sprintf("%s/%s", repo.Owner, repo.Name), nil
}

// ResolveRepo combines ParsePRArg with repoFlag resolution and CurrentRepo fallback.
func ResolveRepo(arg, repoFlag string) (repo string, pr int, err error) {
	repo, pr, err = ParsePRArg(arg)
	if err != nil {
		return
	}
	if repoFlag != "" && repo != "" {
		return "", 0, fmt.Errorf("--repo cannot be used with a full PR URL")
	}
	if repoFlag != "" {
		repo = repoFlag
	}
	if repo == "" {
		repo, err = CurrentRepo()
	}
	return
}

func ParsePRArg(arg string) (repo string, pr int, err error) {
	if strings.HasPrefix(arg, "https://github.com/") {
		// https://github.com/<owner>/<repo>/pull/<number>
		parts := strings.Split(strings.TrimPrefix(arg, "https://github.com/"), "/")
		if len(parts) != 4 || parts[2] != "pull" {
			return "", 0, fmt.Errorf("unrecognised GitHub PR URL: %s", arg)
		}
		n, err := strconv.Atoi(parts[3])
		if err != nil {
			return "", 0, fmt.Errorf("invalid PR number in URL: %s", parts[3])
		}
		return parts[0] + "/" + parts[1], n, nil
	}

	n, err := strconv.Atoi(arg)
	if err != nil {
		return "", 0, fmt.Errorf("expected a PR number or GitHub PR URL, got: %s", arg)
	}
	return "", n, nil
}

func splitRepo(repo string) (string, string, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok {
		return "", "", fmt.Errorf("invalid repo format %q: expected owner/name", repo)
	}
	return owner, name, nil
}
