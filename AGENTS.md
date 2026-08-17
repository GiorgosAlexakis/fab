# Agent instructions

## Working with stack-pr

If you are instructed to use [stack-pr](https://github.com/modular/stack-pr) to manage stacked PRs. Each commit in the branch corresponds to a live PR, and stack-pr embeds tracking metadata directly in commit messages to maintain that link.

### Creating a new stack

When no PRs exist yet for the current branch, first structure all commits, then submit once at the end.

1. **Build all commits before submitting.** Make sure every logical unit of work is its own clean commit with a clear message. Do not run `stack-pr submit` until the full set of commits is ready.

2. **Check what will be submitted:**
   ```bash
   stack-pr view
   ```
   Every commit should show `(No PR)` — this is expected before first submit. Confirm the commit list matches your intent.

3. **Submit the stack:**
   ```bash
   stack-pr submit
   ```
   stack-pr will create one PR per commit and embed tracking metadata into each commit message. After this point, follow the rules below for any further changes.

4. **Never add commits after submitting** to address feedback on an earlier PR. Instead, amend the relevant commit directly (see rules below).

---

### Rules for an existing stack

**Always check the stack before submitting:**
```bash
stack-pr view
```
Verify every commit shows a linked PR number (e.g. `#123`). A commit showing `(No PR)` means its metadata was lost — do not run `stack-pr submit` until this is resolved.

**If any commit shows `(No PR)`, check for existing PRs before proceeding.** Metadata loss can happen silently during rebases or amends. Before creating new PRs, search for existing ones that match the commit titles:
```bash
# List open PRs by the current user and compare titles against local commits
gh pr list --author=@me --state=open --json number,title --jq '.[] | "#\(.number) \(.title)"'
git log --oneline main..HEAD
```
If any PR titles match commit messages, the metadata was lost — do NOT run `stack-pr submit`. Instead, recover the `stack-info` metadata from the remote branches (see "If metadata is accidentally lost" below) and restore it to the commit messages.

**Never create fixup commits.** When a change belongs to an existing commit, amend that commit directly using interactive rebase:
```bash
git rebase -i main
# mark the target commit as `edit`, apply the change, then:
git add <files>
git commit --amend --no-edit  # --no-edit preserves the commit message including stack-info metadata
git rebase --continue
```
Do not use `git commit --fixup` or leave stray fixup/squash commits for later — embed the change in the right commit immediately.

**Never alter or drop stack-pr metadata from commit messages.** Every commit managed by stack-pr contains a `stack-info` line at the bottom of its message, such as:
```
stack-info: PR: https://github.com/ORG/REPO/pull/58, branch: User/stack/45
```
When amending a commit:
- **Always use `--no-edit`** to preserve the commit message: `git commit --amend --no-edit`
- If you must change the message, copy the full original message first (including the `stack-info` line) and include it in the new message
- After the rebase completes, run `stack-pr view` again to confirm all PRs are still linked

**If metadata is accidentally lost**, do not run `stack-pr submit` — it will create duplicate PRs. Instead:
1. Find the original metadata from the remote branch: `git log remotes/origin/<branch> -1 --format='%B'`
2. Or check the PR description on GitHub which lists the branch name
3. Restore with `git commit --amend -m "<full message including stack-info line>"`
4. Verify with `stack-pr view` — every commit must show a linked PR number
