# gh watch

A `gh` extension that watches pull requests and optionally auto-merges them once
all checks pass and approvals are in.

```
gh watch

  PR    READY  AUTO   TITLE                                    REVIEWS  CHECKS   UPDATED
  18      ●           Add dark mode support to settings page    0/1/0    5/0/0       12s
 253      ●    auto   Migrate auth tokens to Redis cache pool   2/0/0    3/2/0       12s

[↑/↓] navigate  [a] add  [x] remove  [m] auto-merge  [r] refresh   [o] open in browser   [q] quit
```

## Installation

```bash
gh extension install nikhilweee/gh-watch
```

Requires the [`gh` CLI](https://cli.github.com) to be installed and
authenticated.

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

Opens a live TUI that polls all watched PRs every 60 seconds and shows their
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

The dashboard is a 7-column table:

| Column    | Meaning                                                                                        |
| --------- | ---------------------------------------------------------------------------------------------- |
| `PR`      | PR number                                                                                      |
| `READY`   | Merge readiness — green: ready · yellow: pending · red: needs your attention                   |
| `AUTO`    | `auto` when auto-merge is enabled, blank otherwise                                             |
| `TITLE`   | PR title (truncated with `…` if the terminal is narrow)                                        |
| `REVIEWS` | Three counts in green / yellow / red: approved · pending · changes-requested. Zeros are muted. |
| `CHECKS`  | Three counts in green / yellow / red: passed · running · failed. Zeros are muted.              |
| `UPDATED` | Time since the last poll                                                                       |

When auto-merge is enabled for a PR, the dashboard merges it as soon as it's
approved, mergeable, and CI is green.

### Keyboard shortcuts

| Key            | Action                                     |
| -------------- | ------------------------------------------ |
| `↑` / `k`      | Move cursor up                             |
| `↓` / `j`      | Move cursor down                           |
| `a`            | Add a PR (enter a PR number or GitHub URL) |
| `x`            | Remove the selected PR from the watchlist  |
| `m`            | Toggle auto-merge on the selected PR       |
| `r`            | Force refresh all PRs immediately          |
| `o`            | Open the selected PR in a web browser      |
| `q` / `Ctrl+C` | Quit                                       |

## Auto-merge

When `--automerge` is set (or toggled in the dashboard with `m`), the dashboard
merges the PR as soon as all of the following are true:

- PR is open and not a draft
- `reviewDecision` is not `CHANGES_REQUESTED` or `REVIEW_REQUIRED`
- `mergeStateStatus` is `CLEAN`
- All CI checks have passed

## State

Watched PRs are stored in `~/.config/gh-watch/state.json`. The file is written
atomically and persists across sessions. PRs are automatically removed from the
watchlist once they are merged or closed.

## Local development

```bash
git clone https://github.com/nikhilweee/gh-watch
cd gh-watch
go build -o gh-watch .
gh extension install .
```
