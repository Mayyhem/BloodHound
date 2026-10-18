# Change 5: extension lanes and role icons on the analysis graph

Repository: the BloodHound clone at the repository root, branch `experimental-postprocessing`. The work is in the Analysis Workspace page, [cmd/ui/src/views/AnalysisWorkspace](../../../cmd/ui/src/views/AnalysisWorkspace). This is analysis-workspace work, not designer work, so it lives here under `tools/esc1-poc/changes` (moved from the designer changes folder on 17 October 2026, designer decision 56). **Status: built, tested, and deployed to the lab on 16 October 2026 for manual testing.** Nothing is staged or committed.

## The problem

Each step on the analysis graph shows a number in its circle, and that number stands for an extension. To find out which extension a step belongs to, you have to open the Extension filter and look up what "2" means. When every step is green, the small namespace text under each node all looks alike. It is also hard to see where one extension depends on another, for example an Active Directory step feeding a CyberArk step.

## The solution

1. **One horizontal lane per extension.** A step's position from left to right is still worked out for the whole graph, exactly as today, so further left still means "runs earlier", even across extensions. Its position from top to bottom is moved into its extension's lane. The extension's name sits at the start of the lane. An arrow that crosses from one lane into another is a dependency between extensions.
2. **Role icons replace the numbers.** The circle shows what the step does, worked out from the outputs it already declares.
3. **The text under each node shows only the version.** The lane already names the extension.
4. **The Extension filter lists names without numbers.**

Status colors (green, orange, red, gray, focus blue) are unchanged. There is no extra failure badge: red already marks a failed step.

## Decisions (questions 1–26, 16 October 2026)

All requirements are agreed. Any new questions continue from 33.

| # | Question | Decision |
|---|---|---|
| 1 | What is the problem? | Can't tell which extension a step belongs to without decoding the number, and relationships between extensions are hard to see. |
| 2 | Who reads it? | Extension authors (1–3 extensions) and administrators (up to about 20). The design must work at 20. |
| 3 | Does every step belong to one extension? | Yes, decided by the step's namespace. Any exception goes in a lane titled "No extension". |
| 4 | Show a lane when only one extension is on screen? | No. With one extension (or the filter narrowed to one), no lanes are drawn. |
| 5 | Is losing left-to-right order across groups acceptable? | **No.** Execution order must stay readable across the whole graph. This is why lanes replaced the boxes from the original proposal. |
| 6 | Lane order, top to bottom | Extensions that others depend on go on top. Ties are broken alphabetically by title. |
| 7 | Fallback if the layout library's grouping is unreliable | Agreed. Lanes no longer use that grouping feature at all (see "How it will work"). |
| 8 | Lane title | The extension's display name, falling back to its namespace. When two extensions share a display name, the namespace is added in small text. |
| 9 | Clicking a group | Filters the table to that extension. |
| 10 | Role icons | Yes. A step with more than one role shows more than one icon (see 22). |
| 11 | Extension filter | Names only, no numbers. Filtering an extension out hides its whole lane. |
| 12 | Failure badge | Not needed. Red already shows failure. |
| 13 | Fading on focus | Lanes with no related steps fade along with their steps. |
| 14 | New, unsaved file in the editor | Goes in the lane of the extension it is being added to. |
| 15 | Shipping | A separate change with its own note. The earlier pending changes have been pushed. |
| 16 | Success test | With the cross-platform example and no legend, you can tell within a few seconds which extension each step belongs to, and point at the AD → CyberArk arrow. |
| 17 | Lanes instead of boxes | Yes. |
| 18 | Lane title placement | Pinned to the left edge of the graph panel, so it stays visible while you pan. It still moves up and down with its lane. **Changed twice after lab reviews:** see 27, then 29–32. |
| 19 | Text under each node | File name, then version only. The namespace line is removed. |
| 20 | Arrows between extensions | Crossing a lane line is enough. No extra color or thickness. |
| 21 | Clicking a lane | Filters the table and highlights that lane, fading the others. Clicking it again, or clearing filters, undoes both. How this fits with the Extension dropdown is settled in 25. |
| 22 | Several roles | Show the actual icons. Two roles sit side by side; all three are arranged in a triangle. |
| 23 | Tall graphs | Fit everything on screen when the graph opens, as today. You zoom in from there. |
| 24 | Lane appearance | A faint alternating background shade plus a thin divider line, light enough that the status colors stay dominant. Uses theme colors so dark mode works. |
| 25 | Clicking a lane vs. the Extension dropdown | A lane click is a lighter, separate action. The dropdown ([AnalysisWorkspace.tsx:439-456](../../../cmd/ui/src/views/AnalysisWorkspace/AnalysisWorkspace.tsx#L439-L456)) filters the table *and* hides other extensions' steps, which under decision 4 would make every lane disappear. A lane click instead filters only the table and fades the other lanes. Nothing on the graph is hidden, so the arrows into and out of that extension stay visible. A small removable chip above the table, such as "Table: CyberArk ×", shows the lane filter is on. "Clear filters" removes it too. The dropdown keeps its current behavior. Rejected: making a lane click pick the extension in the dropdown, which is simpler but loses the arrows between extensions. |
| 26 | Lanes when the search box hides steps | The lanes don't close up. The layout is computed once per plan so things stay where you last saw them. A lane whose steps are all hidden is hidden too, leaving a gap. |
| 27 | Lane title position, after the first lab review | "Anchored to the top middle of each lane", with "a bit more padding" above it. A lane spans the whole canvas width, so the title is centered on the canvas and doesn't move sideways when panning. |
| 28 | Cypher tab, same review | The file box should only scroll when the file runs past the bottom of the entity panel. |
| 29 | What felt jarring about the centered titles | The graph sliding underneath names that stayed still. |
| 30 | Lane title behavior, replacing 27's centering | Titles move with the graph, at the top-left of their lane just before its first step. When panning would push one off screen or under a panel, it stops at the edge of the uncovered area. The top-left panel, the entity panel, and the table (with the button row) all count as covered. |
| 31 | A lane completely under panels | Its title is hidden, and comes back as soon as any part of the lane is uncovered. |
| 32 | Space above the title | Keep the 10 pixels from 27. |

## Alternatives considered

- **Labeled boxes per extension, with steps grouped inside** (the original proposal). This is clear, but each box gets its own left-to-right layout, so position across boxes no longer shows execution order (decision 5). It also relies on the layout library's grouping mode, which is known to misplace or fail on left-to-right graphs with arrows between groups.
- **A colored ring per extension.** This competes with the status colors, needs a legend, and runs out of distinguishable colors after a handful of extensions, especially for colorblind viewers.
- **Short codes inside the node ("AD", "CA").** Easier than numbers, but you still have to decode them, and they collide (two extensions starting with "PoC").
- **Role icons plus the existing namespace text, no lanes.** This was suggested in answer 5. It tells you what a step does but not, at a glance, which extension it belongs to, and it doesn't help with dependencies between extensions. The namespace text is what currently blends together.

## What changed from the plan while building

Two findings from the real lab data changed small details. The decisions above still stand.

- **Where the table icon comes from.** The plan said each step's declared outputs include `query` for "returns rows for other steps". They don't: the server lists only relationship and helper-object outputs. Rows reach another step when that step declares `depends-on: query:...`, and the page already shows each one as a dependency arrow. So the table icon means "another step reads this step's rows", read from those arrows. The first lab deployment showed an empty ring on nearly every step, which is how this was found.
- **Where the lane titles sit (decisions 18 and 27).** The first deployment put each title just left of the graph's first column, because the page's top-left panel covers the left edge of the graph. After seeing it, you asked for the titles to be anchored to the top middle of each lane (decision 27). That made the graph slide underneath still names, which felt jarring (decision 29). The deployed version follows decisions 30–32: titles move with the graph and stop at the edge of whatever the panels leave uncovered.

## How it works

1. **Layout** in `placeInLanes` ([graph.ts:92](../../../cmd/ui/src/views/AnalysisWorkspace/graph.ts#L92)), called at the end of `buildQueryGraph` ([graph.ts:139](../../../cmd/ui/src/views/AnalysisWorkspace/graph.ts#L139)). The existing left-to-right layout runs exactly as before. With two or more extensions, each step then keeps its left-to-right position and is moved up or down into its extension's lane:
   - **Lane order:** `laneOrder` ([graph.ts:51](../../../cmd/ui/src/views/AnalysisWorkspace/graph.ts#L51)) lists which extensions wait on which, from the arrows that cross extensions. It then repeatedly takes the alphabetically first extension (by title) whose prerequisites are already placed. If every remaining extension waits on another (a loop), it takes the alphabetically first one.
   - **Inside a lane:** steps sharing a column are stacked in the top-to-bottom order the layout gave them. A step alone in its column goes on the lane's first row, so a simple chain sits on one line. A lane is as tall as its busiest column.
   - **Spacing:** 160 between rows. Each lane has 125 of room above its first row for the title and 150 below its last row for the name and version under each node ([graph.ts:37-42](../../../cmd/ui/src/views/AnalysisWorkspace/graph.ts#L37-L42)). The room below is larger because node labels shrink more slowly than the graph when zooming out. The first deployment used 90 and 90, which clipped the version line under a lane's bottom row. The room above went from 110 to 125 when the title got more space above it (decision 27), so the title doesn't crowd the first row.
   - **No arrows at all:** each lane lists its steps in rows of four.
   - **One extension:** nothing moves and no lanes are returned, so single-extension plans look exactly as before.
   - Sigma (the graph library) draws a larger vertical value higher on the screen, so lanes are stored with `top` as the larger number ([graph.ts:29](../../../cmd/ui/src/views/AnalysisWorkspace/graph.ts#L29)). This uses no grouping feature of the layout library and adds no dependency.
2. **Lane titles** come from `extensionTitle` and `laneTitles` ([model.ts:23-44](../../../cmd/ui/src/views/AnalysisWorkspace/model.ts#L23-L44)): the display name, or the namespace when there is none, or "No extension" for a step without one. The namespace is added in small text only when two lanes share a display name. Installed extensions are searched too, so a new file's extension has a name in the editor.
3. **Lane drawing** in [QueryGraph.tsx](../../../cmd/ui/src/views/AnalysisWorkspace/QueryGraph.tsx):
   - **Bands:** a layer behind Sigma holds the bands ([QueryGraph.tsx:426](../../../cmd/ui/src/views/AnalysisWorkspace/QueryGraph.tsx#L426)). Sigma's own background is now see-through so the bands show.
   - **Following the view:** after every redraw (pan, zoom, resize), `measure` ([QueryGraph.tsx:183](../../../cmd/ui/src/views/AnalysisWorkspace/QueryGraph.tsx#L183)) converts each lane's edges to screen pixels. React only redraws the bands when one actually moved.
   - **Which lanes show:** `shownLanes` ([QueryGraph.tsx:141](../../../cmd/ui/src/views/AnalysisWorkspace/QueryGraph.tsx#L141)) hides a lane whose steps are all hidden by filters, and draws nothing when fewer than two lanes remain (decisions 4 and 26).
   - **Colors:** `laneShade` ([QueryGraph.tsx:161](../../../cmd/ui/src/views/AnalysisWorkspace/QueryGraph.tsx#L161)) gives every other lane a faint shade of the text color and the clicked lane a faint shade of the focus blue. Each shade comes from the lane's place in the full list, so hiding one lane doesn't recolor the rest. Node labels are drawn on a small patch of background color; that patch now uses the lane's shade so it doesn't show as a box.
   - **Titles (decisions 30–32):** buttons on the main page, and plain text in the editor's view-only window.
     - **Natural place:** 24 pixels left of the lane's first step and 10 pixels below the lane's top edge (`titleLead`, `titleMargin` in [QueryGraph.tsx](../../../cmd/ui/src/views/AnalysisWorkspace/QueryGraph.tsx)), so it moves with the graph like the steps do.
     - **Uncovered rectangles:** these come from the same page measurements Reset Graph uses (`coveredEdges`): one below the top-left panel and one beside it, both stopping before the entity panel and above the table's button row. The editor window has no panels, so only its button row counts.
     - **Stopping at the edge:** `placeLaneTitle` (in [graph.ts](../../../cmd/ui/src/views/AnalysisWorkspace/graph.ts)) checks each uncovered rectangle. It moves the title sideways only as far as needed to stay inside a rectangle, and down only if the lane's top is above that rectangle, without ever leaving its own lane. It prefers sliding sideways, which keeps the title level with the top of its lane, above the lane's steps. It slides down into the lane only when the lane's top is outside every uncovered rectangle, for example scrolled above the canvas. The first try of this rule picked whichever direction moved less. After one pan that dropped a title onto its lane's first row of steps instead of sliding it beside the top-left panel, so the sideways preference was added.
     - **Hidden titles:** when no rectangle can hold the title inside its lane (the lane is off screen, under panels, or squeezed thinner than its title), the title is hidden. A hidden title is still laid out invisibly, so its real width is known when it comes back.
     - **When it is recalculated:** after every Sigma redraw (pan, zoom, resize) and after every page update (a panel opening, closing, or being resized). Collapsing the top-left panel is a plain browser action with no page update, so the panel now triggers an update when it is collapsed or expanded ([AnalysisWorkspace.tsx](../../../cmd/ui/src/views/AnalysisWorkspace/AnalysisWorkspace.tsx), `onToggle`).
4. **Fading.**
   - **Lane titles:** `laneFaded` ([QueryGraph.tsx:173](../../../cmd/ui/src/views/AnalysisWorkspace/QueryGraph.tsx#L173)) fades a title when another lane is clicked, or when a step is focused and none of the lane's steps is on its path (decision 13).
   - **Steps:** a clicked lane also fades the steps in every other lane, the same way focus fades unrelated steps (`outsideLane`, [QueryGraph.tsx:243](../../../cmd/ui/src/views/AnalysisWorkspace/QueryGraph.tsx#L243)).
   - **Arrows:** an arrow into or out of the clicked lane keeps its full color.
5. **Lane click (decisions 9, 21, 25)** in [AnalysisWorkspace.tsx](../../../cmd/ui/src/views/AnalysisWorkspace/AnalysisWorkspace.tsx). A new `laneFilter` value ([AnalysisWorkspace.tsx:95](../../../cmd/ui/src/views/AnalysisWorkspace/AnalysisWorkspace.tsx#L95)) narrows only `tableRows` ([AnalysisWorkspace.tsx:145](../../../cmd/ui/src/views/AnalysisWorkspace/AnalysisWorkspace.tsx#L145)). The graph's shown steps still come from search and the Extension dropdown only. The table's rule count, its "select all", and the "hidden" count of selected rules use the table rows. The lane filter is cleared by:
   - clicking the same title again
   - the chip's ×
   - "Clear filters"
   - a new rule or schema revision
   - picking an extension in the dropdown, which hides the other lanes anyway
   - opening a step outside the lane from a details link, which already clears the other filters
6. **Role icons.**
   - **Roles:** `queryRoles` ([model.ts:52](../../../cmd/ui/src/views/AnalysisWorkspace/model.ts#L52)) gives relationship and helper-object roles from the step's outputs, and the row role from the arrows (see "What changed from the plan").
   - **Images:** `roleIconSvg` ([roleIcons.ts:46](../../../cmd/ui/src/views/AnalysisWorkspace/roleIcons.ts#L46)) draws the matching Font Awesome icons (link, cube, table) in black: one in the middle, two side by side, three in a triangle ([roleIcons.ts:30](../../../cmd/ui/src/views/AnalysisWorkspace/roleIcons.ts#L30)). A step with none of the three roles shows an empty ring.
   - Steps with the same roles share one image ([QueryGraph.tsx:122](../../../cmd/ui/src/views/AnalysisWorkspace/QueryGraph.tsx#L122)).
7. **Text under the node.** The shared label drawing ([node-label.ts:113](../../../cmd/ui/src/rendering/programs/node-label.ts#L113)) used to draw a second line only as "source | kind". It now draws a single ready-made `sublabel` line when one is given. The analysis graph gives the version ([QueryGraph.tsx:266](../../../cmd/ui/src/views/AnalysisWorkspace/QueryGraph.tsx#L266)). Explore never sets `sublabel`, so Explore is unchanged; the field already existed in the shared type.
8. **Numbers removed everywhere.** `numberedExtensions` and the editor's `numberingQueries` setting are gone. The Extension dropdown lists "Display name (namespace)", sorted by namespace.
9. **Cypher tab (decision 28)** in [QueryDetails.tsx](../../../cmd/ui/src/views/AnalysisWorkspace/QueryDetails.tsx). The file box used to have a fixed 600-pixel height with its own scrollbar, leaving empty panel space below a long file. On the Cypher tab the details section, its column, and the file box now fill the rest of the entity panel's height. The box scrolls only when the file is longer than the space left, and its sideways scrollbar sits at the bottom of the panel. A very short panel still gives the file 200 pixels, and the panel itself then scrolls. The other tabs are unchanged.
10. **Editor dependency window** uses the same lanes. A new, unsaved file goes in its extension's lane and shows the empty ring until another step reads its rows or it is saved with outputs.

## New dependencies

None. The icons come from `@fortawesome/free-solid-svg-icons`, which the page already uses.

## Downstream effects

- Only the Analysis Workspace page (graph and Cypher tab), its editor, and one shared label-drawing function change. Explore draws exactly as before.
- Graphs with several extensions are taller. "Fit on open" keeps the whole graph on screen, at a smaller size.
- Some arrows between extensions are longer, because steps that used to sit next to each other may now be in different lanes.
- The guides that described the numbers now describe lanes and icons: [END_USERS.md](../END_USERS.md), [DESIGN.md](../DESIGN.md), and [USAGE.md](../USAGE.md). The [workspace plan](../../../_plans/opengraph-analysis-workspace.md) marks its original numbering instruction as superseded and links here.

## How it was tested

- **Unit and page tests:** `TZ=America/Los_Angeles npx vitest run src/views/AnalysisWorkspace src/rendering` in `cmd/ui`: 11 files, 104 tests, all passed. New or changed tests:
  - [graph.test.ts](../../../cmd/ui/src/views/AnalysisWorkspace/graph.test.ts):
    - left-to-right positions are identical to the old layout
    - one lane per extension, with no overlaps
    - lane order
    - a chain sits on one row
    - steps sharing a column keep the layout's order
    - one extension moves nothing
    - rows of four without arrows
    - dependency loops
    - titles decide ties
    - title placement: stays put while uncovered; stops beside the top-left panel (even when the lane's top is just above the panel's bottom edge), at the left edge, before the entity panel, and at the top of a lane scrolled partly off screen; hidden when the lane is off screen, behind the table, too thin, or the space is too narrow
  - [model.test.ts](../../../cmd/ui/src/views/AnalysisWorkspace/model.test.ts):
    - titles, including duplicate names, installed-only extensions, and "No extension"
    - roles, including the row role from arrows, fixed order, and no repeats
  - [roleIcons.test.ts](../../../cmd/ui/src/views/AnalysisWorkspace/roleIcons.test.ts): the empty ring, each single icon, side by side, and the triangle
  - [AnalysisWorkspace.test.tsx](../../../cmd/ui/src/views/AnalysisWorkspace/AnalysisWorkspace.test.tsx):
    - the filter lists names without numbers and still hides other extensions on the graph
    - a lane click filters only the table and shows the chip, and is undone by clicking again, the chip, or Clear filters
    - the dropdown replaces a lane click
    - a details link outside the lane clears it
- **Explore and shared drawing:** `npx vitest run src/views/Explore src/rendering src/ducks/graph` passed 64 of 65 while the lab build was running in parallel. The one failure, `GraphView > renders a table if the query has NO node edges`, passed when rerun on its own (15 of 15 in that file), so it was a timing failure under load.
- **Type check and format:** `npx tsc --noEmit` passed. Prettier reports no differences. ESLint reports only two warnings that were already in `AnalysisFileEditor.tsx` on lines this change does not touch.
- **Not run yet:** `just test` and `just prepare-for-codereview`. They take a while, so AGENTS.md says to ask first.
- **Real browser, lab deployment:** read-only checks: no page errors, no upload, save, test, activation, or analysis requests. Scripts and screenshots are in `tools/esc1-poc/work/extension-lanes-deploy/`.
  - `browser-check.py`, light and dark mode:
    - four lanes, in this order: Active Directory, CyberArk, ESC1 processing experiment, ESC1 using ordinary Cypher
    - titles run top to bottom and bands don't overlap
    - clicking "Active Directory" changed the table from 29 rows to 1, showed the chip, and kept all four lane titles on the graph; clicking again restored 29 rows and removed the chip
    - lane titles start 10 pixels below their lane's top edge (from the centered version, kept by decision 32)
  - `panels-check.py`, with the top-left panel, the table, and the entity panel all open: titles move with the graph on a small pan, and after panning right, left, up and down no shown title overlaps any panel. Collapsing the top-left panel lets titles move into the uncovered space.
    - on the Cypher tab of `PoC_Cypher/esc1.cypher`, the file box reaches the bottom of the entity panel and scrolls inside only because the file is longer than the panel
  - `editor-check.py`: the editor for `PoC_CyberArk/cyberark_links.cypher` shows two lanes (Active Directory, CyberArk) with plain-text titles.
  - **Screenshots checked by eye:**
    - `ad_users` shows cube and table side by side
    - row steps show the table
    - `esc1` and `cyberark_links` show the link
    - failed `domain_helpers` steps are red with the cube
    - the version line is no longer clipped

## Deployment

App image `esc1-poc-bloodhound-workspace:20261016-extension-lanes-5` (program sha256 `6e4a6fff…093470f5`), app container only, in project `esc1-workspace-manual`. The databases and the rule service (`20261016-partial-runs`) were not restarted; their start times are recorded in `work/extension-lanes-deploy/start-before.txt`. The method is the same as earlier deployments:

1. `BUILD_PATH=<deploy>/ui npx vite build`
2. copy that output into a source copy's `cmd/api/src/api/static/assets`
3. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath ./cmd/api/src/cmd/bhapi`
4. wrap the program with the existing thin Dockerfile

The first build (`…-extension-lanes`) was replaced after the browser check found the empty-ring and clipped-label problems. The second (`…-extension-lanes-2`) was replaced for decisions 27 and 28, the third (`…-3`) for decisions 29–32, and the fourth (`…-4`) for the sideways preference.

**Rollback:**

1. In `tools/esc1-poc/work/workspace-deployment/compose.yaml`, set the `bloodhound` image back to `esc1-poc-bloodhound-workspace:20261016-unchanged-edges`. A copy of the earlier file is `work/extension-lanes-deploy/compose-before.yaml`.
2. From that folder, run `docker compose -p esc1-workspace-manual --env-file deployment.env -f compose.yaml up -d --no-deps --no-build --pull never bloodhound`.

## Manual testing steps

Open <http://127.0.0.1:28080/ui/administration/opengraph-analysis> and sign in as `admin` / `experiment-only`.

1. **Lanes (decisions 16, 17, 6).** Without the legend, name each step's extension from its lane. Find the arrow from `ad_users` (Active Directory lane) to `cyberark_links` (CyberArk lane); it crosses the lane line. Active Directory should be above CyberArk.
2. **Execution order (decision 5).** Steps further left run earlier, across lanes too: `ad_users` is left of `cyberark_links`.
3. **Icons (decisions 10, 22).**
   - `ad_users` shows a cube and a table side by side.
   - `forest_reach` and most ESC1 steps show a table.
   - `esc1` and `cyberark_links` show a link.
   - `cyberark_accounts` and `domain_helpers` show a cube.
4. **Text under nodes (decision 19).** File name, then version only.
5. **Lane click (decisions 9, 21, 25).**
   - Click "CyberArk". The table shows only CyberArk's two rules, "Rules: 2", and a "Table: CyberArk" chip. The CyberArk lane turns faintly blue, other lanes and their steps fade, and the `ad_users` → `cyberark_links` arrow stays dark. Nothing disappears from the graph.
   - Click "CyberArk" again: everything returns.
   - Click it once more, then remove the chip with its ×.
   - Try "Clear filters" as well.
6. **Dropdown (decision 11).** Open Extension: names without numbers. Pick "CyberArk": the other lanes and their steps disappear, and with one extension left no lanes are drawn (decision 4).
7. **Search (decision 26).** Clear filters, then type `esc1` in Search queries. Lanes whose steps are all hidden disappear, and the remaining lanes don't move.
8. **Focus (decision 13).** Click `cyberark_links`. Lanes with nothing on its path (both ESC1 lanes) fade.
9. **Panning and zoom (decisions 23, 30–32).**
   - Open a step so the entity panel is showing, and keep the table open.
   - Each lane's name sits just before its first step, with a little space above it, and moves with the graph when you drag it.
   - Drag the graph left until the names reach the left edge: they stop there instead of leaving the screen.
   - Drag the upper lanes behind the top-left box: their names slide right to just beside the box.
   - Drag toward the entity panel or the table: names stop before them.
   - A lane that is completely behind panels shows no name until part of it comes back into view.
   - Collapse the top-left box: names move into the space it leaves.
   - Press Reset Graph: everything fits again.
10. **Dark mode (decision 24).** Toggle Dark Mode in the left menu. The lanes still alternate faintly, and labels have no visible boxes.
11. **Cypher tab (decision 28).** Open `PoC_Cypher/esc1.cypher`, then the Cypher tab. The file box reaches the bottom of the entity panel, with no empty space below it. A short file such as `ad_users` shows no inside scrollbar. Drag the panel's height smaller (shrink the window) and the box shrinks with it.
12. **Editor (decision 14).** Open `cyberark_links` in the table, then **Edit**. The dependency window shows Active Directory and CyberArk lanes, with titles that are not clickable. Close without saving.

## Suggested commit

After your review, and after `just prepare-for-codereview` (not run yet; it takes a while):

```sh
git add -- cmd/ui/src/views/AnalysisWorkspace/graph.ts cmd/ui/src/views/AnalysisWorkspace/graph.test.ts \
  cmd/ui/src/views/AnalysisWorkspace/model.ts cmd/ui/src/views/AnalysisWorkspace/model.test.ts \
  cmd/ui/src/views/AnalysisWorkspace/roleIcons.ts cmd/ui/src/views/AnalysisWorkspace/roleIcons.test.ts \
  cmd/ui/src/views/AnalysisWorkspace/QueryGraph.tsx cmd/ui/src/views/AnalysisWorkspace/AnalysisWorkspace.tsx \
  cmd/ui/src/views/AnalysisWorkspace/AnalysisWorkspace.test.tsx cmd/ui/src/views/AnalysisWorkspace/AnalysisFileEditor.tsx \
  cmd/ui/src/views/AnalysisWorkspace/QueryDetails.tsx cmd/ui/src/rendering/programs/node-label.ts \
  tools/esc1-poc/END_USERS.md tools/esc1-poc/DESIGN.md tools/esc1-poc/USAGE.md tools/esc1-poc/HANDOFF.md \
  _plans/opengraph-analysis-workspace.md tools/opengraph-designer/changes/05-analysis-graph-extension-lanes.md
git diff --cached --stat
git commit -m "feat: Show extension lanes and role icons on the analysis graph <JIRA-TAG>" \
  -m "Each extension gets its own horizontal lane while left-to-right order stays the execution order across the whole graph. Lane titles filter only the table; the other lanes fade. Node numbers are replaced by icons for what each query produces (relationships, helper objects, rows read by other queries), and the text under each node shows only the version. The Cypher tab's file box now fills the entity panel and scrolls only when the file is longer. Requirements, alternatives, tests, and manual steps are in tools/opengraph-designer/changes/05-analysis-graph-extension-lanes.md." \
  -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
git push
```

Replace `<JIRA-TAG>` before committing. `tools/opengraph-designer/` is otherwise untracked; add only this note unless you also want the others.

## Not in this change

- Collapsing a lane into one node. That is a later phase of the workspace plan; the lanes are built so collapsing can reuse them.
- Any change to what the status colors mean, or a separate failure badge.
