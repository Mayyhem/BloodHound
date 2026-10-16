# OpenGraph analysis migration plan

**Target branches:** a family of small pull request branches named `opengraph-analysis-<topic>` in both BloodHound and DAWGS, plus one local integration branch named `opengraph-analysis` in each repository (see "Pull request plan")

**Source branch:** `experimental-postprocessing` in both repositories

**Status:** Reviewed plan only. Do not execute it until the experimental design is stable and the user approves the migration.

## Purpose

The current experimental branches prove the main ideas, but their commits mix code, tests, generated files, documentation, and large research records. That makes the changes difficult to review. This plan rebuilds the finished design on current `origin/main` as a series of small commits. Each commit should solve one understandable problem and include the tests that prove its behavior. The commits are then grouped into pull requests by one rule: **merge independently useful changes first and on their own; stack only the changes that truly depend on earlier ones.** A reviewer who can understand and approve a change without the rest of the feature gets it as a separate pull request. A reviewer who needs an earlier change to understand or check it gets a stacked pull request on top of that change.

This is a clean migration of the final design. It is not a replay of every experiment. Superseded fields and formats must not be reintroduced just because they appear in an older commit.

TODO 3's future [analysis workspace](../../_plans/opengraph-analysis-workspace.md) starts with a read-only MVP for current activated rules and matching completed reports, using BloodHound's sequential layout and coordinated graph, table, and query details. Its query nodes and dependency arrows must never enter the collected-data graph. Management remains on the current rules page during the MVP; later phases add management, grouping, live/historical inspection, and isolated testing. MVP completion is separate from full TODO 3 completion. The workspace plan does not authorize this migration or a rule-storage move. Add its tested implementation to the migration inventory only when that work exists, without inventing completed commits or additional DAWGS requirements now.

## Decision about timing

Do the full migration after the behavior and author-facing file formats are stable. Migrating now would make later experiments harder because every reversal would need to be repeated on two branches.

Until then:

1. Keep new work small and focused on `experimental-postprocessing`.
2. Keep code, tests, generated files, and nearby documentation together.
3. Preserve the existing research records and their source details.
4. Update this plan when the final design changes.
5. Start the migration only after the user says the experiment is ready for review.

## Rules for the migration

- Create every independent pull request branch from the latest fetched `origin/main`. Create every stacked branch from the branch directly below it. Never put an independent change behind the feature just because it was written during the feature.
- Do not push branches or open pull requests without the user's approval. Pushing publishes the work.
- Back up and prove the backups before fetching or changing either working tree.
- Never run migration commands in the dirty DAWGS source checkout.
- Never use `git clean -fdx`, rewrite history, or discard existing work.
- Do not cherry-pick the large experimental commits. Rebuild the final code by topic.
- Do not restore superseded `external_node_kinds`, the old `endpoints` field, the old ownership fields, or earlier proposal wording.
- Keep historical evidence byte-for-byte unchanged. Put fresh results from the rebased code in a separate migration record.
- Keep generated files in the same commit as the source that generates them.
- Add dependency files in the first commit that needs each dependency. Do not add all dependencies up front.
- Keep every intermediate commit safe. In particular, no commit may expose unchecked queries, partially publish a shared answer, select rules from incomplete change information, or treat an uncertain save as success.
- The agent may edit files and recommend commands. The user stages and commits every change manually.
- Use fresh, isolated database and Compose project names. Do not reuse the application, correctness, comparison, or benchmark databases from the experiments.

## Current source inventory

The BloodHound branch currently contains these ten commits after its locally recorded `origin/main`:

1. `2eb18fe41` — Initial OpenGraph relationship analysis engine
2. `99fb48110` — Shared relationship producers and ownership work
3. `b9fac21c7` — Final schema names and inactive drafts
4. `7d57d4d11` — Query checks and execution reports
5. `253670cde` — Cypher comparison and supporting evidence
6. `f60833782` — PostgreSQL preparation improvement
7. `7110b5c36` — Proposal formatting
8. `da7da1845` — Path-input and reachability follow-up
9. `2f25f4935` — Proposal update
10. `b0a2dc934` — Schema definition proposal update

The DAWGS branch contains committed change `5f16c574` plus tracked and untracked path-input work. Preserve both parts. The three final path-input commits below may be combined if separating them would leave DAWGS unable to build.

The local `origin/main` references were last observed to be stale. Their age means nothing should be rebased or copied until both repositories have been backed up and `origin/main` has been fetched again.

## Work that must remain out of scope

- Do not claim Enterprise support or capacity at 100 million objects.
- Do not claim that browser checks are load tests.
- Do not claim that released BloodHound gives users incorrect native findings. The released DAWGS failure was reproduced for a custom query, but native ESC1 uses a different path.
- Carry the separate TODO 10 write-selection correction and its tests together. The earlier study's explicit-ID cleanup is a historical workaround, not evidence that the general correction existed during that study.
- Do not silently delete helper objects or stale collected facts.
- Do not promise coordination with other BloodHound servers or direct database writers. This design coordinates one modified Community Edition server.
- Do not change extension author requirements without explicit approval.

## Phase 1: freeze and back up the source

Before fetching anything:

1. Record both repository paths, branches, `HEAD` values, remotes, status, submodule/worktree information, ignored-file rules, and current `origin/main` values.
2. Record every changed path with a null-safe file list.
3. Create a Git bundle for each repository.
4. Save binary patches for tracked changes in each repository.
5. Archive untracked files with their paths and file modes. This is required for the seven currently untracked DAWGS files and for any later untracked files.
6. Create SHA-256 manifests for the archives, bundles, patches, and their contents.
7. Restore each backup into a disposable directory and prove that its committed, modified, and untracked content matches the source.
8. Store the backup outside both working trees but inside the `postprocessing` directory.

Stop if any file cannot be represented and restored exactly. Do not fetch as a substitute for a backup.

## Phase 2: fetch and create isolated worktrees

After the backups pass:

1. Fetch `origin` in BloodHound and DAWGS without switching either source checkout.
2. Record the newly fetched `origin/main` commit and date in both repositories.
3. Review upstream changes that touch any file or behavior in this plan. Mark each old change as still needed, already upstream, superseded, or changed by the new base.
4. Create the BloodHound integration worktree on a new local branch `opengraph-analysis` from its fetched `origin/main`. It is where the finished pull requests are combined and tested together. It is never itself a pull request.
5. Make BloodHound commit B01 first, on its own independent branch `opengraph-analysis-workspace`. It keeps the nested DAWGS worktree out of BloodHound and records the migration rules. Merge that branch into the integration branch.
6. Create the DAWGS integration worktree on a new local branch `opengraph-analysis` at `<new BloodHound worktree>/tools/DAWGS`. This location preserves the existing local Go replacements such as `../DAWGS` and `../..`. As each DAWGS pull request branch is finished, merge it into this integration branch.
7. Build each pull request's commits in its own clean worktree, following the "Pull request plan". Do this one pull request at a time, in the order the plan lists.

Git cannot hold both a branch named `opengraph-analysis` and branches named `opengraph-analysis/<something>`, so pull request branches use a hyphen, not a slash.

If any branch name already exists locally or on the remote, stop and inspect it instead of overwriting it. When cleaning up later, remove the DAWGS worktree before removing the BloodHound worktree.

## Pull request plan

Commit numbers (D01, B02, and so on) stay as stable names for the work. This section groups them into pull requests. Pull request titles follow the repository format: `<conventional commit tag>: <Title> <Jira tag>`.

### How each pull request is handled

- **Independent** pull requests go straight to `main`. They do not wait for the feature.
- **Stacked** pull requests follow [GitHub's stacked pull request model](https://docs.github.com/en/pull-requests/get-started/about-stacked-prs). The bottom pull request targets `main`. Each pull request above it targets the branch of the one directly below it, so its diff shows only its own layer. Open them as **drafts**, marked "blocked by" the pull request below, and link each to its neighbors.
- The key rule from GitHub: if code in one layer depends on code in another, the dependency must be in the same branch or a lower one. Start a new layer when the concern changes or the layer is big enough to review on its own.
- Every description says what this layer adds, what the reviewer should focus on, what it deliberately does not do, and what tests were run.
- A stack is one straight chain. The BloodHound layer table below is a tree, so the "main chain" is one stack, and each side branch (BH-1b, BH-6/7, BH-16, BH-17, BH-18) is opened against the branch it hangs from and retargeted to `main` once that branch merges. Side branches merge after their base.
- All branches in a stack must be in the same repository; GitHub does not support stacks across forks. BloodHound and DAWGS are separate repositories, so they can never be in one stack. Cross-repository dependencies (for example BH-10 needing DAWGS pull requests) are handled by links and "blocked by" notes, not by stacking. Before opening anything, confirm with the user whether branches will be pushed to the upstream repositories or to forks. If forks, stacking is unavailable and the layers must be opened one at a time as each base merges.
- Stacked pull requests merge from the bottom up. Merging a middle pull request also merges every pull request below it; the ones above it stay open and retarget automatically. Merging the top pull request merges the whole stack. The stack does not have to merge all at once.
- Rebasing is cascading. Prefer GitHub's server-side cascading rebase or the `gh stack` command-line extension over rebasing branches by hand. After a lower branch changes, rerun the focused tests of every branch above it. When the bottom pull request merges, GitHub rebases the rest so the next one targets `main`. The agent recommends these commands; the user runs them.
- Merge requirements for every pull request in a stack come from the bottom pull request's base branch (`main`). Branch protection rules, code owner approvals, and the CI checks that run for pull requests to `main` apply to every layer, including middle ones. Run `just prepare-for-codereview` per layer so each one passes on its own.
- A pull request may not merge if it leaves a broken or misleading product. Until the settings commit (B45) lands, nothing in a merged layer may run analysis by default. Unused library additions are acceptable because nothing calls them.
- Reorder commits inside a pull request freely if needed for safety, but never move a commit into a lower pull request than the one that supplies what it needs.

### DAWGS pull requests (all independent of each other except where noted)

| PR | Commits | Why this grouping | Target |
| --- | --- | --- | --- |
| DAWGS-1 | D01 | Contributor workflow documentation stands alone. | `main` |
| DAWGS-2 | D02, D05, D08 | PostgreSQL correctness fixes (reused graph IDs, kind caches, exact JSON values) that help every DAWGS user. | `main` |
| DAWGS-3 | D09, D15, D17, D18, D19, D20 | Corrections to existing Cypher translation. Each fixes a wrong or unsafe answer and needs nothing from this feature. | `main` |
| DAWGS-4 | D10, D11, D12, D13, D14, D16 | New standard Cypher support (maps, trim, toFloat, VALUES parts). Additive. D10 comes before D11 and D12. | `main` |
| DAWGS-5 | D21, D22, D23, D24, D25, D26 | Path and input-row corrections. D24 and D25 build on D22, so they stack inside this pull request. D21 changes unbounded path answers and gets its own explanation in the description. May need DAWGS-3 first if the tests share cases. | `main` |
| DAWGS-6 | D03, D04 | Graph write pause and write-activity reporting. Safe library additions that nothing calls until BloodHound uses them. | `main` |
| DAWGS-7 | D06, D07 | Explicit PostgreSQL transactions and guarded batches. Stacked on DAWGS-6 because the batches honor the write guard. | DAWGS-6 branch |

If DAWGS maintainers prefer smaller pull requests, split any row by commit; the commits are already independent enough. BloodHound cannot drop its local DAWGS replacement (B42) until every DAWGS pull request it needs is merged and released.

### BloodHound pull requests

Independent:

| PR | Commits | Notes |
| --- | --- | --- |
| BH-0 | B01 | Workspace preparation and agent rules. No runtime behavior. |
| BH-E1 | B49, B59, B63, B66, B67, B69 | Historical evidence records only, preserved byte-for-byte. Documentation only, so it can merge at any time. Open these last and mark them low priority. Each is its own pull request if reviewers prefer, since each stands alone. |

Stack (each layer is built on the one before it; merge bottom first):

| Layer | Commits | What it adds | Depends on |
| --- | --- | --- | --- |
| BH-1 Schema contract | B02, B03, B04, B05 | The final schema format, complete query checks, output checks, and cross-extension ordering. The shared contract everything else reads. | `main` |
| BH-1b Schema viewer | B06 | Offline model viewer. Stands beside the stack: needs only BH-1 and does not block BH-2. | BH-1 |
| BH-2 Calculation core | B07, B08, B09, B14 | Resource limits first, exact answers, ordinary Cypher rules, and explanation reports. | BH-1, DAWGS-3, DAWGS-5 |
| BH-3 Advanced calculation | B10, B11, B12, B13 | Reachability rules, overlap detection, input batching, and parallel execution. | BH-2 |
| BH-4 Shared results | B15, B16 | Update policies and atomic publishing of shared relationship results. | BH-3 |
| BH-5 Database saving | B17, B18 | Neo4j and PostgreSQL adapters with matching behavior. | BH-4, DAWGS-7 |
| BH-6 Standalone runner | B19, B20, B21, B22 | Collection loading, the command, its container, and the local lab. | BH-5 |
| BH-7 Examples | B23, B24, B25 | ESC1, vault, and cross-extension examples with written answers. | BH-6 |
| BH-8 Rule storage | B26, B27, B28, B29, B30, B31 | Stored revisions, drafts, activation, safe pause and removal, and bounded reports. | BH-5 |
| BH-9 Secured service | B32, B33, B34, B35 | The authenticated API, management page, report history, and signing client. Security checks land with the first endpoint, in B32. | BH-8 |
| BH-10 Native connection | B42, B36 | The DAWGS build link and adapters to BloodHound's real graph access. B42's local DAWGS `replace` line is for local integration only and must never merge. Before BH-10 can leave draft, replace it with the released DAWGS version (a normal version bump with its `go.sum` changes) once the needed DAWGS pull requests are merged and released. | BH-5, all needed DAWGS PRs |
| BH-11 Write coordination | B37, B43, B44 | Pause graph writes during analysis, guard direct writes, restrict maintenance. | BH-10, DAWGS-6 |
| BH-12 Settings and selection | B45, B38, B39 | Settings (feature off by default) come first, then change tracking and selective rule execution. | BH-11 |
| BH-13 Automatic analysis | B40, B41, B48 | Run after native analysis, block writes after uncertain saves, and the manual Run Analysis action. | BH-12, BH-9 |
| BH-14 Packaging and labs | B46, B47 | Persistent service images and disposable labs. | BH-13 |
| BH-15 Documentation | B51, B52, B53 | Author guide, design explanation, and the one final proposal. Merges last among the stack so it describes landed code. | BH-14 |
| BH-16 Cypher comparison | B54, B55, B56, B57, B58, B60 | Ordinary Cypher example, measurement, comparison command, studies, and findings. Optional; may be deferred or dropped without affecting the feature. | BH-7 |
| BH-17 Preparation follow-ups | B61, B62, B64, B65 | Preparation timing, identity-check improvement, and their explanations. | BH-5 |
| BH-18 Path-input explanation | B68 | Plain-English account of the path input correction. | DAWGS-5, BH-2 |
| BH-19 Final validation | B50, B70, B71 | Fresh validation of the integrated branch, and the handoff map. Run on the integration branch after everything else has merged. | all above |

Dependencies between layers on different rows are minimums. Where two layers do not depend on each other (for example BH-6 and BH-8), they can be reviewed in parallel and merged in either order after their shared base.

The safety gates in the next sections still control order inside and between pull requests. If a gate and this table disagree, the gate wins and the table is corrected.

## Commit message format

Use a short conventional-commit subject followed by this plain-English body:

```text
<type>: <short result>

Problem:
<What failed, was missing, or was hard to understand.>

Why:
<Why this solution is the smallest safe choice.>

Change:
<How the commit works.>

Effect:
<What changes for callers, extension authors, or later commits.>

Tests:
<Exact commands and results run for this commit.>
```

Write the `Tests` section from actual results. Never copy planned tests into a commit message as though they passed.

### Pre-commit code cleanup pass (required for every code commit)

The experimental code was written quickly, partly by AI, and is rebuilt here for human reviewers. Before the cleanliness check below, give each commit a deliberate cleanup pass. The goal is code a new reader can follow without help. The agent may use the `simplify` skill as a starting point, but must read the result itself.

1. **Remove what is not used.** Delete unused functions, types, fields, parameters, constants, imports, flags, test helpers, and options that nothing in the final design calls. Delete dead and commented-out code. Do not keep anything "in case it is needed later" or for backwards compatibility; this project has no other users.
2. **Simplify.** Replace long or deeply nested functions with short ones that each do one thing. Remove needless layers, wrappers, and abstractions that exist for only one caller. Prefer the plain, direct way over the clever way. Merge near-duplicate code. Remove defensive checks for cases that cannot happen, but keep checks that guard real failures.
3. **Make it readable.** Use full descriptive names with no abbreviations. Make the order of the file follow the order a reader would think about the problem. Keep functions and files a reviewable size.
4. **Fix the comments.** Every comment explains why, or what happens, in plain English a non-engineer can follow, and it is true to the code beside it. Remove comments that only repeat the code, mention earlier experiments or superseded designs, narrate edits ("added", "now", "fixed"), or contain notes to the agent. Add a short explanation wherever the reason for a choice is not obvious.
5. **Check the tests the same way.** Remove duplicate and unused tests and helpers. Each test name says what it proves, and each test fails when the behavior it covers breaks.
6. **Match the surrounding code.** Follow the repository's existing naming, comment style, and the Go standards in `AGENTS.md` (receiver name `s`, no named returns, grouped `var` blocks, `any` instead of `interface{}`, rich variable names). Generated files are not hand-edited.
7. **Change no behavior.** The cleanup must not change what the code does. Rerun the commit's focused tests after cleanup, and rerun `just test` or `make test_all` where the plan requires it. If cleanup would need a behavior change, stop and raise it as a separate decision.
8. **Report it.** The commit handoff lists what was removed, simplified, or reworded, in a few plain sentences. If nothing needed cleanup, say so.

Historical evidence commits are exempt, because their contents must stay unchanged.

### Pre-commit cleanliness check (required for every commit)

Before the agent hands over any commit for manual staging, it checks the staged-to-be files and reports the result. A commit is not ready until this check is clean or every finding is explained.

1. **Secrets.** Search the exact files and their diff for passwords, bearer tokens, API keys, signing keys, connection strings with credentials, private keys, certificates, cookies, and local `.env` contents. Include test fixtures, Compose files, logs, and screenshots. Use a secret scanner if one is installed (for example `gitleaks` or `trufflehog`), and also read the diff by eye. Test values must be obviously fake and generated at run time where possible.
2. **Files that do not belong in the final change.** List every changed and new path and confirm each one is needed for this commit's purpose. Look specifically for:
   - scratch notes, to-do lists, drafts, and personal documentation;
   - agentic coding instructions and agent state (for example `AGENTS.md` changes, `CLAUDE.md`, `.claude/`, memory or prompt files), except the one deliberate `AGENTS.md` addition in B01;
   - test data, sample collections, database dumps, and generated output that no test or document in this commit uses;
   - editor and operating system files, build output, coverage files, local lab data directories, and backup archives;
   - debugging code, commented-out code, leftover `TODO` markers without an issue, and temporary logging;
   - absolute paths, usernames, email addresses, hostnames, and other personal or machine-specific details.
3. **Local configuration that is wrong for production.** Review every changed configuration, build, and dependency file and confirm nothing only makes sense on this machine or in the lab. Look specifically for:
   - Go `replace` lines pointing to a local path (such as `../DAWGS` or `../..`) in `go.mod` or `go.work`, and `go.sum` changes that come only from them;
   - dependencies pinned to a branch, a commit, or an unreleased version instead of a release;
   - `localhost`, `127.0.0.1`, `bloodhound.localhost`, or the lab ports (`25432`, `27687`) in application code, default settings, shipped configuration, or Docker and Compose files used outside the lab;
   - database names, credentials, or Compose project names from the experiments;
   - features switched on by default, security checks relaxed for testing (disabled authentication, skipped certificate checks, loosened Host/Origin rules), or raised limits and debug flags;
   - build and image settings that assume a local worktree, the nested `tools/DAWGS` checkout, or locally built images;
   - ignore rules, settings, or `.env` files changed for the local setup rather than for the product.

   Lab-only files (labs, studies, examples) may use local values, but only in files that are clearly lab-only and never loaded by the product. Any local value that remains in a commit must be listed in the handoff with the reason it is safe.
4. **Deliberate exceptions.** Historical evidence commits (B49, B59, B63, B66, B67, B69) intentionally include raw results and must stay byte-for-byte unchanged. For them, still run the secrets check, and redact or exclude only with the user's approval, because redaction would alter the evidence. Record any such decision in the evidence manifest.
5. **Report.** The handoff for each commit states: the scanner used (or that none was available), what was searched, every finding (secrets, unneeded files, and local configuration), and what was done about it. Add the "Files changed" list from this check to the commit handoff so the user can compare it with `git status` before staging.
6. **Never fix by hiding.** If a secret is found in a file, tell the user so the secret can be rotated; removing it from the next commit does not remove it from history. Do not stage, push, or open a pull request until the user has decided.

Order for every commit: build the change, run the cleanup pass, run the focused tests, run the cleanliness check, then run `just prepare-for-codereview` (or `make format` and the DAWGS suites). Rerun the affected steps after any later edit to the commit.

Before each BloodHound commit, run `just prepare-for-codereview` as required by the repository instructions. Because that command currently does not run the full tests, also run the focused tests and `just test` for code changes. Inspect generated files before staging.

After each DAWGS code change, run `make format` and focused tests. Before a DAWGS checkpoint, run `make test_all` once with a fresh PostgreSQL `CONNECTION_STRING` and once with a fresh Neo4j `CONNECTION_STRING`. The user must provide or approve those connection strings at execution time. Shared cases must keep the same answers on both databases.

## Ordered commit series

The commit numbers are stable names, not a single required chain. The "Pull request plan" decides which commits travel together and in what order. The default build order is:

1. BloodHound B01, which prepares the nested worktree safely.
2. The DAWGS pull requests, DAWGS-1 through DAWGS-7.
3. The BloodHound stack, BH-1 upward, with independent pull requests built whenever convenient.

If a change is already present on the newly fetched `main`, record that in the migration inventory and omit the empty commit. If two adjacent changes cannot compile independently, combine only those changes and explain why in the final map.

### DAWGS commits

| # | Proposed subject | Problem, choice, and downstream effect | Proof required |
| --- | --- | --- | --- |
| D01 | `docs: Explain review and test rules` | Contributors need one clear workflow. Document formatting, both database suites, shared expectations, and manual commits before moving code. | Review links and commands; documentation checks. |
| D02 | `test: Cover reused graph IDs` | Reusing a graph identifier can expose stale driver state. Add the failing case first so later cache and transaction changes cannot hide it. | Focused driver tests on PostgreSQL; existing graph lifecycle tests. |
| D03 | `feat: Pause graph writes during analysis` | Analysis needs a stable graph while it calculates and saves. Add a graph write gate that waits for active writes and blocks new writes until released. | Race tests for waiting, cancellation, release, and concurrent readers/writers. |
| D04 | `feat: Report graph write activity` | BloodHound cannot coordinate writes it cannot observe. Publish write-start and write-finish events through a small observer interface. | Unit tests for ordering, errors, cancellation, and observer removal. |
| D05 | `fix: Keep PostgreSQL kind caches consistent` | Kind caches can become stale when a transaction changes graph types. Update or invalidate them only after the transaction outcome is known. | Cache tests for commit, rollback, reused graph IDs, and concurrent access. |
| D06 | `feat: Add explicit PostgreSQL transactions` | The analysis publisher must control one complete save. Add an explicit transaction API because a group of writes must commit or roll back together. | Unit and integration tests for commit, rollback, late failure, and cancellation. |
| D07 | `feat: Guard PostgreSQL relationship batches` | Large relationship updates need batching without escaping the caller's transaction or write guard. Add guarded batch methods that share the active transaction. | PostgreSQL integration tests for several batches, failure rollback, and write observation. |
| D08 | `fix: Preserve exact PostgreSQL JSON values` | Ordinary decoding can change large integers or confuse null values. Decode values exactly so answer comparison and saved properties remain truthful. | Tests for nested objects, arrays, large integers, null, invalid JSON, and round trips. |
| D09 | `fix: Reject unsupported Cypher expressions` | Unsupported expressions can otherwise reach translation and fail unclearly or produce unsafe SQL. Reject them during language checking with a useful error. | Negative parser and mutation cases; existing supported expressions still pass. |
| D10 | `fix: Read map parameters` | Queries need to read values such as `$row.id`. Translate map access on parameters rather than treating the parameter as a scalar. | Translation cases and integration answers on both databases. |
| D11 | `feat: Translate Cypher map expressions` | Rules construct maps for rows and relationship properties. Add direct map-expression translation because it preserves ordinary Cypher meaning. | Unit, mutation, and shared template cases for nested and empty maps. |
| D12 | `feat: Read properties from maps` | Property syntax must work for both graph values and maps. Resolve the value type before producing SQL. | Translation cases for nodes, relationships, maps, missing keys, and null. |
| D13 | `feat: Add trim support` | Shipped rules normalize collected strings with standard Cypher `trim`. Add the function instead of requiring an author-specific workaround. | Function translation tests and both-database answer cases. |
| D14 | `feat: Add toFloat support` | Rules need the standard conversion used by the example calculations. Add `toFloat` with null and invalid-input behavior matching the supported reference. | Unit and both-database cases for numbers, strings, null, and invalid input. |
| D15 | `fix: Keep collection filters in scope` | A filter attached to a collection can lose the variable it reads during PostgreSQL lowering. Carry the binding through the correct query level. | Translation and integration cases with list filters and reused names. |
| D16 | `feat: Translate VALUES query parts` | Efficient parameter rows need a safe relational form. Add generated `VALUES` query parts rather than building SQL text from values. | SQL snapshots, parameter binding tests, empty input, and injection-shaped strings. |
| D17 | `fix: Preserve Boolean map values` | Boolean values in parameter maps can be assigned the wrong SQL type. Keep their type when maps are expanded into rows. | Map template cases for true, false, null, and mixed values. |
| D18 | `fix: Distinguish parameters from columns` | Identical names can cause a parameter to be read as a SQL column. Track their origins so each reference resolves to the requested value. | Translation cases with colliding names and both-database exact answers. |
| D19 | `fix: Preserve earlier SET values` | A later `SET` can accidentally read the original value instead of an earlier assignment in the same query. Carry the updated expression forward. | Mutation cases for ordered assignments, null, properties, and created values. |
| D20 | `fix: Sort returned values correctly` | PostgreSQL can sort an internal expression instead of the value returned by the query. Bind the returned expression before applying order. | Translation and integration cases for aliases, nulls, limits, and stable ordering. |
| D21 | `fix: Remove the hidden path depth cap` | A silent 15-edge cap changes unbounded Cypher answers. Remove that hidden answer change while retaining cycle handling, cancellation, and external resource limits. | Cyclic graph tests, paths beyond 15 edges, cancellation, bounded paths, and unchanged shortest-path behavior on both databases. |
| D22 | `fix: Type empty and null path inputs` | Empty or null parameter lists can leave PostgreSQL without a usable row type. Give generated input rows an explicit type and preserve empty answers. | Shared written-answer cases for empty, null, and changing parameter values. |
| D23 | `perf: Reuse selected path endpoints` | PostgreSQL repeated work after it had already found the starting endpoint. Reuse that endpoint because it is exactly the object selected by the query. | Plan inspection and the unchanged membership query's complete answers on both databases. |
| D24 | `fix: Keep path results with their input` | PostgreSQL could join a path found for one `UNWIND` row to another row. Keep each path search inside its input row so result pairs stay correct. | All 33 path-input answer cases on both databases, plus PostgreSQL with optimizations disabled. |
| D25 | `fix: Preserve repeated optional input rows` | Optional matches can collapse separate rows that happen to contain equal values. Number incoming rows privately and join by that identity. | Required and optional matches, repeated inputs, filters, bounds, routes, counts, `SET`, and `CREATE` cases. |
| D26 | `fix: Preserve row selection before writes` | Intermediate WITH steps lost ordering, skipped rows and limits. Retain the selected rows before writes, with sorting after the completed projection and attached filters after a limited selection. | Shared written-answer cases and graph checks on both databases, PostgreSQL with optimizations disabled, failure preservation, parser cases and generated SQL. |

Do not merge D24 into a new reachability feature. It is a correction to ordinary Cypher input handling. D23 through D25 may become one commit only if their internal data structures cannot be separated without an uncompilable intermediate state.

### BloodHound foundation commits

| # | Proposed subject | Problem, choice, and downstream effect | Proof required |
| --- | --- | --- | --- |
| B01 | `chore: Prepare the OpenGraph analysis workspace` | The nested DAWGS worktree must not appear as BloodHound content. Add `/tools/DAWGS/` to `.gitignore`, carry this plan, and add the agreed work rules. This commit creates no runtime behavior. | Clean status with the nested worktree present; documentation and ignore-rule review. |
| B02 | `feat: Define OpenGraph analysis schemas` | Authors need one final schema format. Add `compatible_kinds`, explicit directed endpoint pairs, query definitions, and cross-schema kind resolution without the superseded fields. | Schema parsing and validation tests for native, custom, missing, mutual, and invalid kinds. |
| B03 | `fix: Check complete queries before activation` | Filename and final-column checks miss invalid or unsafe queries. Parse the complete query, check read-only behavior, and report useful file and line errors before activation. | Misspelled clauses, unsupported syntax, hidden writes, undeclared inputs, and valid shipped rules. |
| B04 | `fix: Check rule output contracts` | A runnable query can still return the wrong columns or value types. Check its complete output against the declared result before it can become active. | Missing, duplicate, wrongly typed, nullable, and valid output cases. |
| B05 | `feat: Order rules across extensions` | Rules from different extensions can depend on each other. Build one dependency order across all active schemas because separate per-extension plans cannot preserve those dependencies. | Cross-extension order, missing input, cycle, deterministic tie, and shared producer tests. |
| B06 | `feat: Add offline schema model viewer` | Authors need to inspect several schemas together before uploading them. Generate one offline view using the same final schema parser. | Model snapshots and checks for mutual references, invalid files, labels, and direction. |

### BloodHound calculation engine commits

| # | Proposed subject | Problem, choice, and downstream effect | Proof required |
| --- | --- | --- | --- |
| B07 | `feat: Limit analysis resource use` | Long or broad rules can consume unbounded server resources. Add time, row, input, memory, and concurrency limits before any persistent service can execute rules. | Boundary, cancellation, over-limit, cleanup, and no-partial-save tests. |
| B08 | `fix: Preserve exact query answers` | Comparing lossy decoded values can hide differences between databases. Preserve nested values, large integers, null, invalid data, and full answer hashes. | Exact answer tests and stable distinct sorted-pair hashes on both databases. |
| B09 | `feat: Run ordinary Cypher rules` | Authors should be able to write ordinary read-only Cypher. Add the standard calculation path first so advanced controls remain optional. | Complete rule answers, empty answers, failures, limits, and both database backends. |
| B10 | `feat: Add bounded reachability rules` | Some large traversals need an explicit advanced calculation. Add the reviewed reachability control with clear bounds and cycle behavior, without changing ordinary Cypher. | Direction, bounds, loops, filters, empty starts, repeated starts, limits, and parity cases. |
| B11 | `feat: Detect overlapping relationship scopes` | Two producers can target overlapping endpoint scopes and overwrite each other. Detect overlap before saving and produce one clear plan. | Disjoint, exact, partial, reversed, empty, and multi-extension scopes. |
| B12 | `feat: Batch large rule inputs` | Whole input sets can exceed database parameter limits. Split only the declared input while preserving complete answers and one final save. | Wide-input success, boundary size, repeated rows, failure preservation, and answer hashes. |
| B13 | `feat: Run independent ready rules together` | Unrelated calculations need not wait for a slow rule. Run up to the reviewed limit together, then serialize work that must see newly created helpers. | Dependency timing, concurrency cap, cancellation, failure, and deterministic output. |
| B14 | `feat: Explain each analysis run` | Authors need to know why a rule ran or was skipped. Record decisions, timings, counts, limits, and calculation results without secrets or raw sensitive values. | Report tests for every decision and redaction case. |
| B15 | `feat: Apply relationship update policies` | Authors need declared behavior when an edge already exists. Apply replace, update-details, or keep policies consistently within the compatible endpoint scope. | Existing edge origins, properties, all policies, disagreement, and both databases. |
| B16 | `feat: Publish shared relationship results atomically` | Several rules may produce one relationship type. Combine every producer's complete answer and save it once so a partial producer cannot erase another producer's result. | Shared producer success, failure, rollback, deterministic precedence, policy agreement, and retry. |

### BloodHound database and standalone commits

| # | Proposed subject | Problem, choice, and downstream effect | Proof required |
| --- | --- | --- | --- |
| B17 | `feat: Save analysis results in Neo4j` | The engine needs a Neo4j adapter with scoped, all-or-nothing publishing. Keep database behavior behind the common interface. | Fresh Neo4j integration tests for create, update, remove, rollback, limits, and exact properties. |
| B18 | `feat: Save analysis results in PostgreSQL` | PostgreSQL needs the same meaning using DAWGS transactions and exact JSON handling. Implement parity rather than backend-specific author rules. | Matching PostgreSQL integration tests and cross-backend exact answers. |
| B19 | `feat: Load OpenGraph collections` | The standalone experiment needs real OpenGraph inputs. Reuse BloodHound's accepted collection shape and validate before graph writes. | Valid, invalid, repeated, mixed, and rejected collection cases. |
| B20 | `feat: Add standalone analysis runner` | The engine needs a simple way to load, calculate, and save without changing BloodHound yet. Add one command around the tested packages. | Command tests for configuration, validation, success, failure, and exit codes. |
| B21 | `feat: Package the standalone runner` | Reviewers need a repeatable executable environment. Add a small container that contains only the runner and required files. | Image build, non-root/runtime checks, startup, health, shutdown, and no embedded secrets. |
| B22 | `test: Add a local analysis lab` | End-to-end behavior needs a disposable environment. Add isolated Compose projects with fixed loopback-only test ports and separate data directories. | Start, readiness, complete run, stop, state inspection, and port safety checks. |
| B23 | `test: Add the ESC1 example` | Reviewers need a complete security example with known results. Add the schema, rules, data, and written answers together. | Exact nodes, relationships, properties, empty variants, and both backends. |
| B24 | `test: Add the vault example` | Shared helpers and relationship properties need a smaller understandable example. Add the vault case and its expected graph. | Baseline and changed collection answers on both backends. |
| B25 | `test: Add the cross-extension example` | Cross-extension order and shared values need visible proof. Add the Active Directory and CyberArk example as two separate extensions. | Load-order independence, cross-schema links, reruns, and both backends. |

### BloodHound persistent rule-service commits

| # | Proposed subject | Problem, choice, and downstream effect | Proof required |
| --- | --- | --- | --- |
| B26 | `feat: Store active rule revisions` | Automatic analysis needs a durable, identifiable active definition. Store immutable revisions and atomically choose the active one. | Restart, concurrent read, failed update, revision identity, and corruption cases. |
| B27 | `feat: Save incomplete uploads as drafts` | Authors may upload related schemas in stages. Store incomplete work as inactive drafts so it cannot affect the graph. | Missing schema, later completion, restart, replacement, and active-revision preservation. |
| B28 | `feat: Review and activate complete drafts` | Valid files should not become active without an explicit review. Recheck the complete draft and switch revisions only after approval. | Changed-after-review, validation failure, cycle, successful activation, and rollback. |
| B29 | `feat: Pause related rule producers safely` | Disabling one shared producer can make the combined answer incomplete. Pause the related producer group while preserving files and existing results. | Shared and unrelated producers, dependencies, restart, and report reasons. |
| B30 | `feat: Remove shared rules safely` | Deleting rules must not erase results still justified by another producer. Rebuild the complete affected producer set before cleanup. | Single/shared producer deletion, dependent rules, failed cleanup, and retry. |
| B31 | `feat: Keep bounded execution reports` | A single summary loses failure history and saved-revision truth. Keep 20 attempts and the last confirmed saved revision with clear calculation/save states. | Retention boundary, restart, old revision, calculation failure, confirmed failure, uncertain save, and report failure. |
| B32 | `feat: Secure the local analysis API` | The worker API can run powerful graph queries. Require a fixed-size bearer secret, constant-time comparison, strict methods and JSON, body limits, loopback Host/Origin checks, server-held HMAC keys, no redirects, and generic errors. | Authentication timing shape, malformed requests, oversized bodies, Host/Origin, redirect, method, signature, replay, and secret-redaction tests. |
| B33 | `feat: Add the rule and draft management page` | Authors need to inspect, upload, enable, disable, and delete rules without rebuilding containers. Add a simple local page over the secured API. | Browser workflow checks, escaping, filtering, sorting, multi-select actions, and failure messages. |
| B34 | `feat: Show analysis report history` | Authors need calculation and save details beside their rules. Show bounded attempts and the last confirmed saved revision without exposing secrets or raw graph data. | Page checks for success, skipped, failed, uncertain, old revision, and empty history. |
| B35 | `feat: Sign BloodHound analysis requests` | The later BloodHound server must authenticate to the worker without exposing its signing key to the browser. Add a small signed client with bounded retries. | Signature, expiry, replay, time skew, no redirect, response limits, redaction, and retry behavior. |

### BloodHound application-integration commits

| # | Proposed subject | Problem, choice, and downstream effect | Proof required |
| --- | --- | --- | --- |
| B36 | `feat: Connect analysis to native graph access` | The experimental engine must use BloodHound's real graph managers rather than separate credentials. Add adapters without changing current callers. | Adapter unit tests and both-database integration tests. |
| B37 | `feat: Coordinate BloodHound graph writes` | Analysis can be wrong if imports or deletions change the graph halfway through a run. Coordinate native and extension writes with the DAWGS write gate. | Same-turn analysis/extension writes, pending writers, scoped private privilege, cancellation waiting for physical writes, and known rollback release. |
| B38 | `feat: Track changed graph types` | Running every rule after every upload is expensive. Record bounded object and relationship type changes, including updates and deletions, without per-object growth. | Property/type changes, deletion, repeated upload, bounds, tracking failure, and restart. |
| B39 | `feat: Select rules only for known changes` | Selective execution is safe only when the change set is complete. Use it for fully known changes and fall back to all enabled rules on first run, restart, revision change, raw access, overflow, failure, or uncertainty. | Compare selected and full runs on both databases; every fallback reason appears in the report. |
| B40 | `feat: Run extension analysis after native analysis` | Extension rules must see the graph produced by BloodHound's own analysis. Start them only after native success and keep native behavior unchanged when the feature is disabled. | Native success/failure, disabled setting, order, cancellation, and saved-result checks. |
| B41 | `fix: Block writes after uncertain saves` | A lost connection during commit leaves the graph state unknown. Keep graph writes blocked until inspection or restart; retry a failed report separately without rerunning a confirmed save. | Known rollback releases; uncertain outcome blocks; inspection/restart recovery; report-only retry. |
| B42 | `build: Use the OpenGraph DAWGS worktree` | BloodHound now reaches unreleased DAWGS APIs. Add the root DAWGS replacement and the PoC module requirement in the first root commit that imports them, with `go.sum` changes beside the dependency. **Local integration only:** the replacement points to a local path and must not merge. The pull request version of this commit requires the released DAWGS version instead. | Clean module graph, builds from the nested worktree, and no unrelated dependency changes. For the pull request version: no `replace` lines and no local paths. |
| B43 | `fix: Guard direct BloodHound graph writes` | Some writes bypass the main import path. Put tag deletion, raw PostgreSQL deletion, database switching, and migration-related writes behind the same coordination boundary where applicable. | Tests for each path, pending writers, cancellation, and release after failure. |
| B44 | `fix: Restrict maintenance during analysis` | Maintenance that changes graph state cannot run during a protected analysis. Return a clear busy result or wait according to the existing operation contract. | Maintenance, migration, switch, shutdown, timeout, and no-deadlock cases. |
| B45 | `feat: Add OpenGraph analysis settings` | Operators need an explicit off switch, worker address, secret source, and limits. Add validated settings with the feature disabled when no address is configured. | Defaults, invalid values, secret redaction, disabled behavior, and config loading. |
| B46 | `build: Package persistent analysis services` | The application and worker need repeatable images and startup wiring. Package only the reviewed services and pass secrets at runtime. | Image builds, health, dependency order, non-root/runtime checks, and shutdown. |
| B47 | `test: Add persistent service labs` | Reviewers need disposable Neo4j and PostgreSQL setups that exercise restarts and rule changes. Keep each project isolated from earlier labs and studies. | Full workflow on both databases, restart, draft activation, rule removal, and cleanup. |
| B48 | `feat: Add a manual Run Analysis action` | Users need to recalculate already stored data without uploading a collection. Add a one-time server connection setup and a manual action that uses the same guarded pipeline. | Authorization, disabled state, concurrent run, status, success, failure, and browser checks. |

### BloodHound evidence and main documentation commits

| # | Proposed subject | Problem, choice, and downstream effect | Proof required |
| --- | --- | --- | --- |
| B49 | `docs: Preserve the initial experiment evidence` | The original observations explain why the design exists. Copy source IDs, hashes, patches, screenshots, successes, and known failures byte-for-byte without presenting them as current-main validation. | Manifest comparison against the source branch and valid structured files. |
| B50 | `test: Record the complete OpenGraph pilot` | The integrated behavior needs one reproducible record. Run the final standalone and persistent workflows in fresh projects and save bounded, reviewable results. | Exact answers, revision states, failure preservation, container/image IDs, and cleanup state. |
| B51 | `docs: Explain extension authoring` | Authors need one simple path from schema and `.cypher` files to visible results. Document the final contract, ordinary Cypher default, optional controls, examples, limits, and errors. | Every command and example checked against the migrated code. |
| B52 | `docs: Explain the OpenGraph analysis design` | Reviewers need to understand order, publishing, write coordination, failure states, selection, and scope. Explain why each boundary exists and state the known limits. | Cross-check every claim and link against code and tests. |
| B53 | `docs: Clarify the OpenGraph analysis proposal` | The three late proposal commits describe one final proposal. Fold them into one concise document using the final schema language and no superseded wording. | Review against schemas, author guide, design, and current limitations. |

### BloodHound Cypher comparison commits

| # | Proposed subject | Problem, choice, and downstream effect | Proof required |
| --- | --- | --- | --- |
| B54 | `test: Add the ordinary Cypher ESC1 example` | Performance claims need a readable version without advanced instructions. Add ordinary queries with the same requested result. | Complete exact answers on both databases. |
| B55 | `feat: Measure analysis phases` | Total time alone cannot show where work occurs. Record preparation, calculation, and save phases with bounded measurement data. | Timer tests, failure paths, and no change to calculation answers. |
| B56 | `test: Record exact query answers` | Counts can match while the actual rows differ. Observe normalized complete answers and stable hashes without exposing secrets. | Nested values, large integers, null, invalid values, sorted pairs, and hash stability. |
| B57 | `feat: Add the Cypher comparison command` | Repeatable comparisons need one checked command instead of manual steps. Add load, run, observe, and export operations with explicit database selection. | Command tests, safe defaults, resume behavior, failures, and output validation. |
| B58 | `test: Add repeatable Cypher comparison studies` | The research procedure must be reviewable. Add scripts with resource monitors, fixed repetitions, clear stop conditions, and isolated projects. | Dry runs, interrupted resume, readiness, monitor behavior, and source manifests. |
| B59 | `docs: Preserve historical Cypher results` | The 852 observations and failed attempts are useful evidence from the old base. Preserve them unchanged and label their exact source versions and limits. | Hash and record-count checks; retain failed and stopped studies. |
| B60 | `docs: Explain the Cypher comparison findings` | Readers need measured results separated from inference. Report exact answers, phase times, wide-input behavior, limits, and the studies that do not support a firm speedup. | Recalculate tables from evidence and verify every source link. |

### BloodHound preparation and reachability follow-up commits

| # | Proposed subject | Problem, choice, and downstream effect | Proof required |
| --- | --- | --- | --- |
| B61 | `feat: Measure analysis preparation steps` | Earlier phase totals could not identify setup cost. Add timers around database setup, transaction opening, rule selection, identity checks, and rejected validation. | Unit tests for timers on success and failure; unchanged answers. |
| B62 | `perf: Reduce identity check preparation` | PostgreSQL fetched all type names before checking the narrow migration exception. Check the kind first while preserving whole-graph identity validation. | Missing, empty, duplicate, unrelated, migration-exception, late failure, and both-database tests. |
| B63 | `docs: Preserve preparation study evidence` | The targeted measurements and plans support the change but came from the experimental base. Preserve them unchanged with source and environment details. | Hashes, structured-file validation, and source-version checks. |
| B64 | `docs: Explain preparation findings` | A 24.4% PostgreSQL preparation change must not be described as an equal whole-run improvement. Explain the measured phase, variation, Neo4j overlap, and limits. | Recalculate reported figures from evidence. |
| B65 | `docs: Record the reachability diagnosis` | The investigation found an input-association bug instead of a need for a new algorithm. Explain the query plans, why direct correction was chosen, and why modern `ANY` remains a separate proposal. | Links to exact queries, plans, answers, and TODO 13 resolution. |

### BloodHound path-input follow-up commits

| # | Proposed subject | Problem, choice, and downstream effect | Proof required |
| --- | --- | --- | --- |
| B66 | `test: Record released path input failures` | DAWGS v0.9.0 and v0.9.1 fail the custom two-input query differently from the modified branch. Preserve that narrow result without claiming native BloodHound impact. | Exact tags and commits, query, database, SQLSTATE, and source manifest. |
| B67 | `test: Measure corrected path input handling` | The DAWGS correction needs exact-answer and focused performance evidence. Preserve the 33-case matrix, optimizer-disabled repeat, membership plans, and complete example hashes. | Validate record counts, answer hashes, both backends, and source versions. |
| B68 | `docs: Explain the path input correction` | Reviewers need a plain-English account of wrong input/path pairing and the direct fix. Explain the 267 versus 26,967 intermediate rows without promising a reliable whole-run speedup. | Recalculate figures and cross-check DAWGS behavior and TODO statuses. |

### BloodHound write-selection follow-up commit

| # | Proposed subject | Problem, choice, and downstream effect | Proof required |
| --- | --- | --- | --- |
| B69 | `docs: Record the write selection correction` | The earlier comparison found an unsafe limited deletion. Preserve the separate correction evidence, explain supported writes and remaining forms, and update TODO 10 without revising historical results. | Both database results, failure checks, source fingerprints and unchanged earlier evidence. |

### BloodHound migration validation and handoff commits

| # | Proposed subject | Problem, choice, and downstream effect | Proof required |
| --- | --- | --- | --- |
| B70 | `test: Validate OpenGraph analysis on current main` | Historical results do not prove the rebased branch. Run a smaller fresh correctness and safety set on the exact new commits and store a separate manifest. | Both database suites, exact ESC1/vault/cross-extension answers, query validation, limits, shared producers, write coordination, uncertain saves, rule selection fallbacks, containers, and image/commit IDs. |
| B71 | `docs: Record the OpenGraph migration handoff` | Reviewers need the final map from old work to new commits and a clear list of remaining risks. Record every old path/hunk as migrated, upstream, superseded, or historical evidence. | Fresh status, commit map, test results, limitations, unresolved TODOs, and link checks. |

## Safety gates that control the order

The following rules take priority over commit numbering:

1. Complete query and read-only validation must exist before any activation or execution path can reach a rule.
2. Resource limits must exist before a persistent worker can execute rules.
3. Shared producers must be supported from the first commit that can publish more than one producer for a relationship type.
4. A producer group must agree on its edge policy before saving.
5. An uncertain save must block graph writes until inspection or restart. A known rollback may release the gate.
6. A failed report write retries the report only. It must not rerun a confirmed graph save.
7. Selective execution is permitted only when the complete change set is known. Any uncertainty runs all enabled rules and records the reason.
8. Removing the silent path depth cap must retain cycle handling, cancellation, and explicit resource limits. Shortest-path meaning must remain unchanged.
9. Keep the PostgreSQL write-selection correction separate from the earlier study workaround. D26 must retain the selected objects and their number on both databases; explicit-ID cleanup alone does not prove that behavior.
10. Security checks for the local API must land with the first reachable endpoint, never as a later hardening commit.

## Mapping the old history to the new series

| Old source | Planned destination |
| --- | --- |
| BloodHound `2eb18fe41` | B01 through B52, split by the final behavior rather than original file order. |
| BloodHound `99fb48110` | B05, B11, B15, B16, B29, B30, and their final documentation. Do not restore its superseded ownership fields. |
| BloodHound `b9fac21c7` | B02, B06, B27, B28, and B51. Use `compatible_kinds`; do not restore `external_node_kinds` or `endpoints`. |
| BloodHound `7d57d4d11` | B03, B04, B14, B31, B34, and related documentation. |
| BloodHound `253670cde` | B54 through B60. Keep large results separate from implementation commits. |
| BloodHound `f60833782` | B61 through B65. |
| BloodHound `da7da1845` | B66 through B68 plus DAWGS D22 through D25. |
| BloodHound `7110b5c36`, `2f25f4935`, and `b0a2dc934` | One final B53 proposal commit. |
| DAWGS `5f16c574` | D01 through D21, omitting anything already on the fetched main. |
| Uncommitted DAWGS path-input work | D22 through D25. Preserve tracked and untracked content through the backup. |
| Uncommitted TODO 10 follow-up | DAWGS D26 and BloodHound B69. Preserve it separately from the earlier path-input source and historical evidence. |

During execution, maintain a path-and-hunk inventory. Every old change must end in one of four states: present in a new commit, already upstream, deliberately superseded by the final design, or retained only as historical evidence. An unexplained omission blocks completion.

## Validation policy

Use the smallest useful test while building each commit, then broaden testing at real integration boundaries.

### DAWGS

- Run `make format` after every code edit.
- Run focused unit, translation, mutation, template, and driver tests for the touched behavior.
- Keep shared template expectations identical on Neo4j and PostgreSQL.
- Run the full race/unit and integration suites on fresh PostgreSQL and Neo4j databases before the DAWGS series is handed to BloodHound.
- Compare coverage with the starting branch and do not accept a decrease.
- For D24 and D25, run all 33 path-input cases on both databases and repeat PostgreSQL with its optimizer disabled.
- For D26, run the shared write-selection cases and failure-preservation checks on both databases, repeat PostgreSQL selection with optimizations disabled, and include the generated SQL fixtures.

### BloodHound and the pilot

- Run focused package tests after each code change.
- Run `just test` for BloodHound code boundaries because `just prepare-for-codereview` currently performs generation and review preparation but does not prove all tests.
- Run `just prepare-for-codereview` before every manual BloodHound commit, then inspect all generated changes.
- Use a fresh database for each integration purpose. Never share application, correctness, comparison, or benchmark state.
- Keep the pilot's single-graph safety check. Do not delete nonempty graphs to make a test pass.
- Use the fixed loopback-only test ports already reserved by the pilot: PostgreSQL `25432` and Neo4j `27687`, unless the fetched main creates a conflict.
- Record exact commit IDs, image IDs, database settings, commands, results, stopped state, and cleanup state.

### Required final checks

- Exact complete answers for ESC1, vault, and cross-extension examples on both databases.
- Shared producer merge, conflict, removal, rollback, and retry behavior.
- Full query validation and output validation before activation.
- Resource limits, cancellation, and no partial save.
- Write coordination for imports, direct writes, tag deletion, raw PostgreSQL deletion, database switching, and applicable migrations.
- Known rollback releases the gate; uncertain save keeps it closed.
- Rule selection matches a full run when changes are known and falls back for every uncertain case.
- Local API authentication, request limits, signatures, Host/Origin rules, redirects, and redaction.
- Restart behavior for revisions, drafts, reports, and blocked uncertain state.
- `git diff --check` on new code and documents only. Do not use it to rewrite historical patches or evidence.

## Evidence policy

Historical evidence explains what happened on the experimental commits. It does not validate current `main`.

- Keep historical source IDs, hashes, patches, raw results, failed attempts, and interruption records unchanged.
- Keep the known failed file-count attempt and interrupted studies. They are part of the research record.
- Do not format or fix whitespace inside historical patches.
- Store a manifest beside each evidence group that identifies the BloodHound commit, DAWGS commit, database image, settings, and collection source.
- Put fresh migration results under a new migration-validation directory. Do not overwrite old result files.
- Keep large raw evidence in its own commits. The existing evidence is large by line count but small enough by byte size that Git LFS is not currently required.

## Upstream movement during the migration

Fetch `origin/main` again before final handoff and whenever a pull request lands. If either main branch moved:

1. Inspect the new commits that overlap this work.
2. Integrate them without rewriting or discarding the source work.
3. Rebase each open stacked branch onto the one below it, from the bottom up (use GitHub's cascading rebase or `gh stack`), and rerun its focused tests.
4. Rerun every affected focused and integration test.
5. Update the old-to-new map, the pull request table, and the fresh validation manifest.
6. Do not claim the integration branch is current until both recorded base commits match the fetched refs used for final testing.

When a pull request merges, GitHub retargets the pull request above it to `main`. Mark it ready only after its own checks pass. Side branches (not part of the main chain) must be retargeted by hand.

## Proposed permanent `AGENTS.md` addition

Keep the migration rules in the BloodHound root `AGENTS.md`, which is loaded for work started from this repository. The existing DAWGS instructions still apply when a separate session starts directly in the DAWGS repository. Add this section during B01:

```text
## OpenGraph analysis work

- Split non-trivial work into small, working changes. Each change should solve one problem and include its tests, generated files, and documentation.
- Send changes that stand alone straight to `main` as their own pull requests. Stack a change on another only when it cannot be understood or tested without it. Open stacked pull requests as drafts, link them to their neighbors, and say what each layer adds and what to focus on.
- Implement the final design directly. Add dependencies in the first change that uses them.
- Agents must not stage or commit changes. Show the user the changed files, actual test results, and exact suggested commands.
- Before the checks below, clean up AI-generated code: remove unused code, simplify complex code, make names and structure readable, and rewrite comments in plain English that match the code. Do not change behavior; rerun the tests afterward.
- Before handing over any commit, check the changed files for secrets, for files that do not belong in the final change (scratch notes, personal documentation, agent instructions or state, unused test data), and for local configuration that is wrong for production (local-path dependency replacements, localhost addresses, lab ports, relaxed security, features on by default). Report what was checked and found.
- For BloodHound code, run focused tests and `just test`. Also run `just prepare-for-codereview` before each commit; do not describe it as proof of tests it did not run.
- For DAWGS code, run `make format` after edits and test PostgreSQL and Neo4j whenever query meaning changes. Keep each Cypher correction and its tests and generated SQL fixtures together.
- Ordinary Cypher is the default author experience. Advanced controls remain optional and need measured, documented reasons. Get explicit approval before changing extension author requirements.
- Keep every intermediate change safe. Test failure behavior for authentication, graph writes, queries, limits, selection, and shared results. Do not expose secrets, complete queries, or raw graph identifiers in reports or errors.
- Preserve historical evidence exactly. Store fresh results separately with their source versions and database state. Use isolated worktrees and databases for migrations and comparisons.
```

The one-time requirement to branch from the latest `origin/main` belongs in this migration plan, not in permanent repository instructions.

## References

- [About stacked pull requests](https://docs.github.com/en/pull-requests/get-started/about-stacked-prs) (GitHub Docs). Source for the stack rules in "How each pull request is handled": same-repository requirement, base-branch targeting, the dependency-in-same-or-lower-layer rule, bottom-up merging, cascading rebase and `gh stack`, and how branch protection and CI apply to every layer. Stacks are not supported in GitHub Desktop.
- Merge independently useful, atomic changes first and stack only dependent ones: the principle this plan's "Pull request plan" applies. A reviewer who can approve a change without the rest of the feature gets it as an independent pull request.

## Manual review and commit workflow

For each planned commit, the agent should stop after producing a reviewable working-tree change and provide:

1. The problem and final behavior in plain English, and which pull request and layer this commit belongs to.
2. The exact files changed.
3. The cleanup pass summary, the actual tests and results, and the pre-commit cleanliness check result (secrets scan, unneeded-file review, and local-configuration review).
4. Any material risk or known limit.
5. Exact `git add` commands naming only that commit's files.
6. The full proposed commit message using the template above.

The user reviews, stages, and commits. After the user confirms the commit, continue to the next item. Never use a broad `git add .` for this migration.

## Completion criteria

The migration is complete only when:

- Every independent pull request branch starts from the recorded current `origin/main`, and every stacked branch starts from the branch below it.
- Every code commit had its cleanup pass: no unused code, no needless complexity, and comments that are plain and true to the code.
- Every commit passed the pre-commit cleanliness check, and a final scan of each pull request's full diff found no secrets, no files that should not ship, and no local-only configuration (in particular, no local DAWGS `replace` line).
- Each pull request is described, linked to its neighbors, and marked draft or blocked until its base lands.
- The integrated result, with every pull request merged in dependency order, has been verified across BloodHound and DAWGS together.
- Every source change is mapped to a new commit, upstream code, a superseded design, or immutable evidence.
- Each commit builds and its focused tests pass.
- DAWGS passes its full required checks on both databases with no coverage loss.
- BloodHound passes focused tests, `just test`, and `just prepare-for-codereview` at the required boundaries.
- Fresh end-to-end validation passes on Neo4j and PostgreSQL.
- The final documentation describes the actual code and states all known limits.
- No source checkout, historical evidence, user data, or unrelated work was changed or lost.
- The user has reviewed and manually committed every step.

