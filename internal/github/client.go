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
	ReviewDecision   string `json:"reviewDecision"`
	MergeStateStatus string `json:"mergeStateStatus"`
	Author           string
	BaseRefName      string `json:"baseRefName"`
}

type PollResult struct {
	PR              PRStatus
	PassedChecks    int
	FailedChecks    int
	RunningChecks   int
	ApprovedReviews int
	ChangesReviews  int
	PendingReviews  int
}

type WatchTarget struct {
	Repo string
	PR   int
}

// prResponsePR is the shape of a pullRequest field returned by the GraphQL query.
type prResponsePR struct {
	ID               string `json:"id"`
	Number           int    `json:"number"`
	Title            string `json:"title"`
	State            string `json:"state"`
	ReviewDecision   string `json:"reviewDecision"`
	MergeStateStatus string `json:"mergeStateStatus"`
	BaseRefName      string `json:"baseRefName"`
	Author           struct {
		Login string `json:"login"`
	} `json:"author"`
	LatestReviews struct {
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
}

const prFieldsQuery = `
      id number title state reviewDecision mergeStateStatus baseRefName
      author { login }
      latestReviews(first: 50) { nodes { state } }
      reviewRequests(first: 50) { totalCount }
      commits(last: 1) {
        nodes {
          commit {
            statusCheckRollup {
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
`

func parsePollResult(pr prResponsePR) *PollResult {
	result := &PollResult{
		PR: PRStatus{
			NodeID:           pr.ID,
			Number:           pr.Number,
			Title:            pr.Title,
			State:            pr.State,
			ReviewDecision:   pr.ReviewDecision,
			MergeStateStatus: pr.MergeStateStatus,
			Author:           pr.Author.Login,
			BaseRefName:      pr.BaseRefName,
		},
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
		for _, node := range pr.Commits.Nodes[0].Commit.StatusCheckRollup.Contexts.Nodes {
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
	return result
}

func buildBatchQuery(targets []WatchTarget) (string, map[string]interface{}) {
	vars := make(map[string]interface{}, len(targets)*3)
	var sb strings.Builder
	sb.WriteString("query(")
	for i := range targets {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "$owner%d: String!, $name%d: String!, $number%d: Int!", i, i, i)
	}
	sb.WriteString(") {\n")
	for i, t := range targets {
		owner, name, _ := splitRepo(t.Repo)
		vars[fmt.Sprintf("owner%d", i)] = owner
		vars[fmt.Sprintf("name%d", i)] = name
		vars[fmt.Sprintf("number%d", i)] = t.PR
		fmt.Fprintf(&sb,
			"  pr%d: repository(owner: $owner%d, name: $name%d) {\n    pullRequest(number: $number%d) {\n%s    }\n  }\n",
			i, i, i, i, prFieldsQuery)
	}
	sb.WriteString("}")
	return sb.String(), vars
}

func GetAllPRStatuses(targets []WatchTarget) ([]*PollResult, []error) {
	if len(targets) == 0 {
		return nil, nil
	}
	client, err := api.DefaultGraphQLClient()
	if err != nil {
		errs := make([]error, len(targets))
		for i := range errs {
			errs[i] = err
		}
		return make([]*PollResult, len(targets)), errs
	}

	query, vars := buildBatchQuery(targets)

	var rawResp map[string]json.RawMessage
	queryErr := client.Do(query, vars, &rawResp)

	results := make([]*PollResult, len(targets))
	errs := make([]error, len(targets))
	for i := range targets {
		key := fmt.Sprintf("pr%d", i)
		raw, ok := rawResp[key]
		if !ok {
			errs[i] = queryErr
			continue
		}
		var repoData struct {
			PullRequest prResponsePR `json:"pullRequest"`
		}
		if err := json.Unmarshal(raw, &repoData); err != nil {
			errs[i] = err
			continue
		}
		results[i] = parsePollResult(repoData.PullRequest)
	}
	return results, errs
}

func GetPRStatus(repo string, prNumber int) (*PollResult, error) {
	results, errs := GetAllPRStatuses([]WatchTarget{{Repo: repo, PR: prNumber}})
	return results[0], errs[0]
}

func (r *PollResult) IsReady() bool {
	return r.PR.State == "OPEN" && r.PR.MergeStateStatus == "CLEAN"
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
