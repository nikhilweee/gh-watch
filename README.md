# gh watch

A `gh` extension that watches pull requests and optionally auto-merges them once
all checks pass and approvals are in.

```
gh watch

  PR    STATUS   AUTOMERGE  TITLE                                    REVIEWS  CHECKS   UPDATED
  18    ready              Add dark mode support to settings page    0/1/0    5/0/0       12s
 253    ready    enabled   Migrate auth tokens to Redis cache pool   2/0/0    3/2/0       12s
 301   merged              Refactor auth middleware                  2/0/0   14/0/0        3m

[↑/↓] navigate  [a] add  [x] remove  [m] auto-merge  [o] open  [r] refresh  [s] settings  [q] quit
```

## Installation

```bash
gh extension install nikhilweee/gh-watch
```

Requires the [`gh` CLI](https://cli.github.com) to be installed and
authenticated.

To update:

```bash
gh extension upgrade watch
```

## Usage

### Watch a PR

```bash
# From inside a repo (uses current repo)
gh watch add 2143

# With a full GitHub URL
gh watch add https://github.com/owner/repo/pull/2143

# With an explicit repo
gh watch add 2143 --repo owner/repo

# Enable auto-merge (merges automatically once ready)
gh watch add 2143 --automerge
```

### Open the dashboard

```bash
gh watch
```

Opens a live TUI that polls all watched PRs every 5 minutes by default and shows their
current status. The layout is responsive to terminal width.

### Remove a PR

```bash
gh watch remove 2143
gh watch remove https://github.com/owner/repo/pull/2143
```

### List watched PRs

```bash
gh watch list
```

## Dashboard

The dashboard shows a configurable set of columns. Available columns:

| Column    | Meaning                                                                                        | Default |
| --------- | ---------------------------------------------------------------------------------------------- | ------- |
| `PR`      | PR number                                                                                      | ✓       |
| `STATUS`  | GitHub merge state (see below)                                                                 | ✓       |
| `AUTOMERGE` | `enabled` when auto-merge is on, blank otherwise                                             | ✓       |
| `TITLE`   | PR title (truncated with `…` if the terminal is narrow)                                        | ✓       |
| `AUTHOR`  | PR author login                                                                                |         |
| `BASE`    | Target branch name                                                                             |         |
| `REVIEWS` | Three counts in green / yellow / red: approved · pending · changes-requested. Zeros are muted. | ✓       |
| `CHECKS`  | Three counts in green / yellow / red: passed · running · failed. Zeros are muted.              | ✓       |
| `UPDATED` | Time since the last poll                                                                       | ✓       |

### STATUS values

| Value      | Color  | Meaning                                      |
| ---------- | ------ | -------------------------------------------- |
| `ready`    | green  | All requirements met — safe to merge         |
| `blocked`  | red    | Branch protection rules not satisfied (reviews, checks, or other policies) |
| `conflict` | red    | Merge conflict must be resolved              |
| `behind`   | yellow | Branch is behind the base branch             |
| `unstable` | yellow | Non-required checks are failing              |
| `draft`    | muted  | PR is a draft                                |
| `merged`   | muted  | PR has been merged                           |
| `closed`   | muted  | PR was closed without merging                |

Merged and closed PRs remain in the list with all columns dimmed until manually
removed with `x`.

Column visibility, sort order, and poll interval are persisted to
`~/.config/gh-watch/config.json`.

Press `s` to open the settings overlay. From there you can:

- **Poll interval** — choose from 15s / 30s / 1m / 2m / 5m / 15m / 1h (`space` to
  select)
- **Columns** — toggle visibility with `space`; reorder visible columns with
  `shift+↑/↓`
- **Sort** — press `s` on any sortable column to cycle ascending → descending →
  off

When auto-merge is enabled for a PR, the dashboard merges it as soon as its
STATUS shows `ready`.

### Keyboard shortcuts

| Key            | Action                                     |
| -------------- | ------------------------------------------ |
| `↑` / `k`      | Move cursor up                             |
| `↓` / `j`      | Move cursor down                           |
| `a`            | Add a PR (enter a PR number or GitHub URL) |
| `x`            | Remove the selected PR from the watchlist  |
| `m`            | Toggle auto-merge on the selected PR       |
| `o`            | Open the selected PR in a web browser      |
| `r`            | Force refresh all PRs immediately          |
| `s`            | Open settings                              |
| `q` / `Ctrl+C` | Quit                                       |

## Auto-merge

When `--automerge` is set (or toggled in the dashboard with `m`), the dashboard
merges the PR as soon as GitHub reports `STATUS = ready` — meaning GitHub itself
considers the PR fully mergeable (all required reviews approved, all required
checks passed, no conflicts, branch protection rules satisfied).

## State

Watched PRs are stored in `~/.config/gh-watch/state.json`. Column and sort
preferences are stored in `~/.config/gh-watch/config.json`. Both files are
written atomically and persist across sessions.

## Local development

```bash
git clone https://github.com/nikhilweee/gh-watch
cd gh-watch
go build -o gh-watch .
gh extension remove watch  # if already installed
gh extension install .
```
