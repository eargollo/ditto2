# Duplicate File Finder — UX Research

**Date:** 2026-02-25
**Purpose:** Survey existing duplicate finder tools to inform Ditto's web UI design

---

## 1. Tool-by-Tool Analysis

### 1.1 dupeGuru

**Source:** [dupeGuru documentation](https://dupeguru.voltaicideas.net/help/en/results.html), [rapidseedbox guide](https://www.rapidseedbox.com/blog/dupeguru-guide), [GitHub](https://github.com/arsenetar/dupeguru)

#### How duplicate groups are presented

Results appear as a flat table with an implicit grouping: the **reference file** (the canonical copy to keep) sits at the top of each group without a checkbox, and its duplicates are indented beneath it. Groups are not visually bordered — the visual grouping comes entirely from the row indentation and a disabled checkbox on the reference row.

Two view modes:
- **Normal view** — shows reference file + all its duplicates together
- **Dupes Only** — hides the reference file, shows only the duplicates; useful when sorting by a specific column (e.g., folder) to batch-select all duplicates in one directory

#### Selection model

- Checkbox per duplicate row (reference row has no checkbox — key safety feature)
- `Space` toggles the mark state of all currently *selected* (highlighted) rows
- "Mark All" in the Edit menu marks every duplicate in the list
- There is no Selection Assistant; users must do this manually or via "Mark All"
- Distinction between *selected* (blue highlight, for multi-row operations) and *marked* (checkbox, for deletion) — this dual-state model causes confusion for new users

#### File preview

- A separate **Details panel** (a floating window) shows metadata for the currently highlighted row vs. its reference
- In Picture mode, the Details panel also renders a small image thumbnail for comparison
- No lightbox; no side-by-side full-resolution comparison
- No video preview

#### Statistics and progress

- Progress bar during scan; no breakdown by pipeline stage
- Delta Values mode: numerical columns (size, dimensions, mtime) show the *difference* from the reference in orange rather than the absolute value — very useful for spotting which copy is smaller or older
- Summary count at the bottom of the results list (number of marked files, total size)

#### Good patterns

- Reference file is protected from deletion — cannot be marked; this is the strongest safety affordance in any desktop tool
- Delta Values mode is an excellent idea for helping users quickly decide which copy is the worse one
- Dupes Only + sort-by-folder combination is a fast workflow for per-directory cleanup

#### Bad patterns

- The dual selection/marked state is confusing and undocumented on screen
- No side-by-side image comparison
- The "reference = biggest file" default is opinionated and not always correct (a highly-compressed duplicate can be bigger)
- Interface looks dated; no visual grouping between duplicate groups in the table

---

### 1.2 Duplicate Cleaner Pro

**Source:** [DC manual](https://www.duplicatecleaner.com/manual4/duplicate_files_tab.htm), [feature list](https://www.digitalvolcano.co.uk/dcfeatures.html), [tutorial](https://pictureecho.com/blog/duplicate-cleaner-pro-5-tutorial/)

#### How duplicate groups are presented

Results appear in a table. Groups are numbered (Group 1, Group 2, …) and alternating row background colors distinguish group boundaries. A Group column is always visible. Users can sort by any column while retaining the group number as a tie-breaker.

Three display modes:
- **Details (grid)** — tabular list with columns, alternating row colors per group
- **Details (Grouped)** — same table but with explicit dividing lines between groups
- **Icons/thumbnails** — grid of image thumbnails, grouped

A **Classification view** lets users filter by file type (Images, Audio, Video, Documents) and shows per-category statistics.

Visual emphasis: files over 100 MB are shown in **orange text**; files over 1 GB in **red text**. This passive highlighting helps users spot high-value targets without any extra action.

#### Selection model

- Checkbox per file row
- **Selection Assistant** sidebar — the standout feature of this tool
  - "Select all but one in each group" (one-click bulk selection)
  - "Select oldest files in each group" / "Select newest"
  - "Select files in specific folder" (keep files on one drive, delete the others)
  - "Select files with lowest bitrate" (for audio)
  - Custom filter rules by filename pattern or path regex
- Status bar shows running count of marked files and total size to be removed
- "Select All" / "Select All Except First" / "Deselect All" quick buttons

#### File preview

- A **Preview Pane** shows a thumbnail and metadata when a row is selected
- **Side-by-Side Compare** opens a dedicated window showing two images at once — the most useful image comparison UI in the desktop category
- Double-clicking opens the file in its native application
- Metadata for images includes dimensions, date taken, color depth, camera model

#### Statistics and progress

- Progress bar during scan
- Status bar: file count marked, total size to be freed
- Classification view: per-category count of duplicate groups and wasted space
- No historical trends; no per-stage pipeline progress

#### Good patterns

- Selection Assistant with preset rules is the gold standard for bulk selection — eliminates hours of manual clicking
- Side-by-side image comparison in a dedicated window
- Large file size color coding (orange / red) immediately draws the eye to high-value targets
- Grouped alternating row colors are clearer than dupeGuru's indentation approach
- The Classification view filter (Images / Audio / Video) is well-suited to photo/video library cleanup

#### Bad patterns

- No web UI; desktop only
- Selection Assistant is a separate sidebar panel rather than being inline with results — not obviously discoverable

---

### 1.3 Gemini 2 (Mac)

**Source:** [MacPaw product page](https://macpaw.com/gemini), [Macworld review](https://www.macworld.com/article/2434914/gemini-2-duplicate-file-finder-review.html), [TechJury analysis](https://techjury.net/product-analysis/gemini-2/)

#### How duplicate groups are presented

Gemini 2 prioritizes simplicity above all. After scanning, the user reaches a "Review Results" screen with two top-level categories:
- **All Duplicates** — exact byte-for-byte identical files
- **All Similars** — files detected as visually/perceptually similar (photos taken seconds apart, music tracks that are near-identical)

Within each category, files are organized into sub-categories: Images, Videos, Music, Documents, Folders. Each sub-category shows a count and aggregate size.

Two view modes:
- **List view** — compact rows with thumbnail and metadata
- **Icon/grid view** — larger thumbnails for visual browsing

Selecting a group in the list opens a detail pane showing all copies; hovering a thumbnail shows file metadata.

#### Selection model

- **Smart Cleanup** — one-button automatic mode; the app applies its "Smart Select" algorithm to auto-select which files to remove based on a learned model of the user's past decisions (e.g., always keep the largest version, always keep the file in a specific folder)
- **Review Results** — manual mode; user can inspect group-by-group and override the Smart Select choices
- Smart Select is learnable: it observes patterns ("you always keep files in /Photos/Library and delete from /Downloads") and applies them to subsequent selections
- Users can also define their own Smart Select rules (e.g., "always delete files in /tmp")

#### File preview

- **Built-in preview window** — clicking any file shows it in a quick look pane
- For similar photos, Gemini shows them side-by-side with difference indicators
- Scans run in real-time and show current scan location + combined file size during the scan
- No explicit video preview beyond poster frames

#### Statistics and progress

- Real-time scan progress: current location, combined file size, overall percentage
- Pause/resume during scan
- Result summary: total duplicates found, space that can be freed, number of similar files
- No historical trend charts

#### Good patterns

- **Smart Select** is the most sophisticated auto-selection UX in the category — it learns, which dramatically reduces repeated decision-making across large libraries
- The Duplicate Monitor (background watching) catches new duplicates as they appear — a proactive pattern
- Separating "Duplicates" from "Similars" prevents false positives from cluttering the primary workflow
- The minimal, award-winning UI (Red Dot Design Award) reduces cognitive load
- Real-time feedback during scan (current file being processed) gives the user confidence that something is happening

#### Bad patterns

- macOS-only; no web UI
- Smart Select learning is opaque — users cannot always see why a file was selected for deletion
- Paid subscription ($19.95/year); cost creates friction for casual use

---

### 1.4 AllDup

**Source:** [AllDup help](https://www.alldup.de/alldup_help/alldup.php), [cisdem review](https://www.cisdem.com/resource/alldup-duplicate-file-finder.html), [tutorial](https://easyfilerenamer.com/blog/2024/04/24/alldup-duplicate-file-finder-tutorial/)

#### How duplicate groups are presented

AllDup uses a classic three-panel Windows Explorer-style layout:
- **Left panel** — folder tree for browsing scan locations
- **Center/top panel** — results table showing duplicate groups
- **Center/bottom or right panel** — file preview for the selected item

Groups are labeled in the results table; the interface is described as feature-dense. Column visibility is fully customizable. Users can switch between a flat list and a folder-tree view of results.

The results table can be filtered, sorted, and exported to CSV/TXT/Excel — a differentiator for power users who want to audit duplicates in a spreadsheet.

#### Selection model

- Manual checkbox selection
- Automated selection rules: "select oldest in group", "select newest in group", "select files by path pattern", etc.
- Supports selecting by size range, file extension, modification date range, and custom exclusion masks (including ZIP/RAR/7Z contents)
- Right-click context menu with additional actions per file

#### File preview

- Built-in file viewer for many formats
- Image preview exists but the preview window is **fixed-size and cannot be enlarged** — a noted UX weakness that prevents confident comparison for photo cleanup
- No side-by-side comparison

#### Statistics and progress

- Progress bar during scan
- File count and size summary in status bar
- Export capability (CSV/Excel) for reporting — unique among desktop tools

#### Good patterns

- Archive contents scanning (ZIP, RAR, 7Z) — unique capability
- CSV/Excel export for auditing large libraries before deletion
- Highly configurable column display and customizable UI colors/fonts
- Windows Explorer metaphor is familiar to the target user

#### Bad patterns

- Fixed-size, non-resizable preview window is a significant usability problem for photo library work
- The interface is described as overwhelming for new users — too many options visible at once
- Windows-only

---

### 1.5 Web-Based and Open Source Tools

#### Czkawka (desktop, open source)

**Source:** [GitHub](https://github.com/qarmin/czkawka), [v7.0 release notes](https://medium.com/@qarmin/czkawka-7-0-a465036e8788)

The closest open-source equivalent to the commercial tools above. Two GUI frontends: the older GTK frontend and the newer **Krokiet** (Slint-based, released in v7.0, 2024).

Key UX points:
- Left panel tabs for each scan mode (Duplicates, Similar Images, Similar Videos, Music)
- Results in a sortable table with path, size, mtime
- Right-side preview panel: selecting a file shows a thumbnail
- Supports Similar Images mode (perceptual hashing), which flags visually similar photos regardless of resolution or filename — critical for photo library cleanup
- Multi-hash-algorithm support (Blake3, CRC32, XXH3)
- Fully offline, no telemetry

This is the spiritual predecessor most similar to what Ditto is building.

#### fclones-gui (desktop, GTK4)

**Source:** [GitHub](https://github.com/pkolaczk/fclones-gui), [Hacker News discussion](https://news.ycombinator.com/item?id=36494327)

Designed to complement the `fclones` CLI tool. Workflow is deliberately simple:
1. Add directories → scan
2. Results appear as a list of file groups
3. Select files → choose removal action from dropdown → execute

Early-stage project; limited features but fast due to Rust backend. The Hacker News discussion noted that the CLI's batch-oriented model made it hard to interactively pick which copy to keep — which is exactly the problem a web UI must solve.

#### dude (Duplicates Detector)

**Source:** [GitHub](https://github.com/PJDude/dude)

Uses a **two-panel synchronized layout** — the most distinctive UX in the open source category. Files are presented in two panels side-by-side, where selecting a group in one panel highlights its counterpart in the other. This is borrowed from the classic "two-pane file manager" pattern (Total Commander, Midnight Commander) and is well-suited to power users comparing two folder trees.

#### immich_duplicate_finder (web-based)

**Source:** [GitHub](https://github.com/vale46n1/immich_duplicate_finder)

A Streamlit web app that integrates with the Immich API. Key UX feature: a **comparison slider** for side-by-side image review. Workflow:
1. Enter Immich server URL and API key in the sidebar
2. Run detection
3. Results shown with slider comparison for each duplicate pair

Currently Streamlit-based (limited layout control); a React frontend is on the roadmap. This is the only tool in the survey with a genuine browser-based interface, though it is tightly coupled to Immich.

#### immich-deduper (web-based, Dash/Plotly)

**Source:** [GitHub](https://github.com/RazgrizHsu/immich-deduper)

Uses Dash by Plotly for the UI (after evaluating Streamlit as too inflexible). Reads from Immich thumbnails for ML similarity detection. Not a general-purpose duplicate finder but illustrates the pattern of a self-hosted web UI for NAS photo deduplication.

---

## 2. UX Pattern Summary: What Works and What Does Not

### What works well

| Pattern | Where seen | Why it works |
|---|---|---|
| **Reference file protection** — the canonical copy cannot be selected for deletion | dupeGuru | Prevents the most catastrophic user error; zero cognitive overhead |
| **Sorted by reclaimable space** — largest savings shown first | Implied by all tools' "select all but one" logic | Lets users capture 80% of savings in the first 20% of effort |
| **Selection Assistant / auto-select rules** | Duplicate Cleaner Pro, AllDup | Eliminates manual clicking for large libraries; "keep newest in each group" handles 90% of photo library use cases |
| **Smart learned selection** | Gemini 2 | Reduces repetition across sessions; ideal for users with consistent folder structures |
| **Delta values on numerical columns** | dupeGuru | Instantly shows "this copy is 2.3 MB smaller" without arithmetic |
| **Large file size color coding** | Duplicate Cleaner Pro | Passively draws attention to high-impact rows |
| **Side-by-side / slider image comparison** | Duplicate Cleaner Pro, immich_duplicate_finder | Critical for photo library work; thumbnail alone is often insufficient |
| **Separating exact duplicates from similar files** | Gemini 2 | Different confidence levels; exact duplicates can be auto-handled, similars need review |
| **Per-category statistics** | Duplicate Cleaner Pro, Gemini 2 | "Images: 4.2 GB wasted" gives immediate context before diving in |
| **Progress transparency during scan** | Gemini 2, dupeGuru | Real-time feedback (current file, bytes read) reduces anxiety on long scans |
| **Soft deletion (Trash)** | All tools | Universal safety net; without it many users will not use the tool |
| **Keyboard-driven mark/unmark** | dupeGuru (Space key) | Essential for power users going through hundreds of groups |

### What does not work well

| Anti-pattern | Where seen | Why it fails |
|---|---|---|
| **Fixed-size, non-resizable image preview** | AllDup | Cannot reliably distinguish quality differences in a tiny thumbnail |
| **Dual selected/marked state** | dupeGuru | Two independent states (highlighted row vs. checked checkbox) with no on-screen legend confuses users |
| **No visual grouping between duplicate groups** | dupeGuru (indentation only) | Easy to lose track of which reference file a duplicate belongs to |
| **Overwhelming option density on first screen** | AllDup | Advanced options should be hidden behind a disclosure; defaults should handle 90% of cases |
| **Opaque auto-selection** | Gemini 2 Smart Cleanup | When the app auto-selects files and the user cannot see why, trust erodes |
| **No historical progress tracking** | All desktop tools | Users have no way to know if they are making a dent in the problem over time |
| **No video preview** | Most tools | For NAS users with video libraries, previewing a poster frame before deletion is important |

---

## 3. Recommended UX Patterns for Ditto (Tailwind CSS, NAS Photo/Video Library)

Ditto is targeting a single user on a trusted home network who needs to efficiently clean up a large (1M+ file) NAS library — primarily photos and videos. The web UI should optimize for **batch decision speed** while preventing irreversible mistakes.

### 3.1 Dashboard Page

**Outcome metrics first, not process metrics.** The user cares about "how much space am I wasting?" and "how much have I already freed?" — not the technical pipeline.

Recommended layout (top to bottom):

```
[Large stat cards in a row]
  "X duplicate groups"  |  "Y GB reclaimable"  |  "Z GB freed all-time"

[Trend sparklines]
  Reclaimable space over the last N scans (line chart, minimal)

[Scan status bar — visible only when a scan is running]
  "Scanning... 142,000 files found · 3,400 hashed · 12 GB read"
  [Progress bar with stage labels: Walk → Hash → Done]
  [Cancel] button

[Last scan summary + "Scan Now" button]

[Scan history table — collapsible]
```

Key decisions:
- Use large, readable stat cards (Tailwind `text-4xl font-bold`) for the primary metrics — not tiny text in a sidebar
- Show the trend chart only if there are at least 3 historical scans (otherwise hide it — a single data point is not a trend)
- During an active scan, replace the "Scan Now" button with live counters using HTMX polling or SSE; update every 2 seconds
- Show a per-stage progress breakdown (matching the §5.6 counters in the requirements): Walk, Partial Hash, Full Hash, Done — this builds confidence and helps diagnose slow scans

### 3.2 Duplicate Groups List

**The most important UX decision: default sort order.**

Sort by reclaimable space descending. This means the user's first ten minutes of work captures the majority of savings. Group 1 might be "50 copies of raw video · 40 GB reclaimable"; Group 1000 might be "2 copies of a README · 1 KB reclaimable". Most users will never reach Group 100.

Recommended row layout:

```
[Thumbnail (80×80 px, lazy-loaded)]
[File type icon if no thumbnail]

Group #1
IMG_0042.jpg  ·  JPEG  ·  5.2 MB per copy
12 copies  ·  52 MB reclaimable

[Badge: IMAGE]          [Auto-select rule ▼]   [Review →]
```

Recommendations:
- Lazy-load thumbnails — do not attempt to load all 10,000 thumbnails on page load
- Show a file type badge (IMAGE / VIDEO / DOCUMENT) in a color-coded pill using Tailwind (`bg-blue-100 text-blue-700` etc.)
- Separate the list into two sections: **Exact Duplicates** (same SHA256 hash) and, in a future version, **Similar Files** (perceptual hash). In v1, only exact duplicates exist, but the section header primes the user for v2.
- Provide a filter bar: file type (All / Images / Videos / Documents), minimum reclaimable space (e.g., "only show groups where savings > 10 MB"), status (Unresolved / Resolved / Ignored)
- Show a "bulk action" toolbar that appears when groups are selected via checkbox: "Auto-select oldest in all selected groups" / "Mark all for deletion" / "Ignore selected groups"

### 3.3 Group Detail View (the core workflow screen)

This is where the user spends most of their time. It must be fast and reduce cognitive load for repetitive decisions.

**Two-tier layout:**

Tier 1 — File comparison cards (the primary decision area):
```
[Full-width or 2-column grid of file cards]

Card:
  ┌─────────────────────────────┐
  │  [Image preview / 200×200]  │
  │  or [Video poster frame]    │
  ├─────────────────────────────┤
  │  /volume1/photos/2023/      │
  │  IMG_0042.jpg               │
  │  5.2 MB · 2023-07-14        │
  │  4032×3024 px               │
  ├─────────────────────────────┤
  │  [KEEP]  (radio button)     │
  └─────────────────────────────┘
```

Key decisions for the card:
- Use **radio buttons** for the "keep" selection, not checkboxes. One and only one file in the group must be kept. Radio buttons make the mutual-exclusivity obvious at a glance.
- The "DELETE" state is implicit — any card that is not the selected KEEP is deleted. This is opposite to the checkbox-per-delete model of desktop tools, which requires users to mark every unwanted copy. With 12 copies, checking 11 checkboxes is exhausting; clicking one radio button is instant.
- Show a `[KEEP]` badge in green on the currently selected radio; show `[DELETE]` badge in red on all others. Use Tailwind `ring-2 ring-green-500` to visually distinguish the kept card.
- For images: clicking the thumbnail opens a **lightbox / full-resolution preview** (can use a simple modal). For videos: show a poster frame with a play button that streams a short clip via the `/api/preview` endpoint.
- Show **delta indicators** alongside metadata: if a copy is smaller than the largest copy, show `-2.1 MB` in orange. If a copy is older, show the age difference. This is borrowed from dupeGuru's delta values feature.

Tier 2 — Actions and whitelist controls (below the cards):
```
[Confirm: Keep /volume1/photos/2023/IMG_0042.jpg, delete 11 others]
[Delete selected  ↓]   [Skip this group →]   [Ignore …▾]
                                               ├ Ignore this group
                                               ├ Ignore this file hash
                                               └ Exclude directory
```

Key decisions:
- The "Delete selected" button should show a confirmation summary: "Keep 1 file · Delete 11 files · Free 57 MB"
- "Skip this group" advances to the next unresolved group without marking anything — important for cases where the user is unsure
- The Ignore menu covers the three whitelist cases from the requirements (§7.4)
- After confirming deletion, automatically advance to the next group without requiring the user to navigate back to the list

**Keyboard shortcuts** for power users going through hundreds of groups:
- `→` / `Enter` — skip to next group
- `←` — go back to previous group
- Number keys `1` / `2` / `3` etc. — select which copy to keep
- `D` — confirm deletion with current selection
- Show a small keyboard shortcut legend at the bottom of the page

### 3.4 Navigation and Progress Within the Cleanup Session

**Session progress indicator.** The user needs to know how much work remains. Provide a persistent progress bar at the top of the duplicate groups list and the detail view:

```
"Reviewing: 47 / 1,204 groups resolved  ·  14.2 GB freed this session"
[████████░░░░░░░░░░░░░░░]  4%
```

**Jump navigation.** Allow the user to filter by file type or minimum savings mid-session without losing their position. A sidebar filter panel (hidden on mobile, visible on large screens) with:
- File type checkboxes
- Minimum reclaimable space slider
- "Show only unreviewed groups" toggle (default: on)

### 3.5 Image Comparison: Slider vs. Cards

Two comparison patterns exist in the survey:
- **Comparison slider** (immich_duplicate_finder) — overlays two images with a draggable divider; great for near-identical photos but only works for two files at a time
- **Side-by-side cards** (Duplicate Cleaner Pro) — works for any number of copies; scales to groups with 10+ copies

Recommendation: use side-by-side cards as the primary pattern (scales to N copies). Add an optional "compare two" button that opens a full-screen slider modal for the two selected cards — this gives power users the detailed comparison without forcing it on everyone.

### 3.6 Scan Progress Page

During an active scan (which can take 30+ minutes on 1M+ files), show a dedicated progress view accessible from the dashboard:

```
Scan in progress — started 14 minutes ago

Stage              Files           Status
─────────────────────────────────────────
Walk               1,243,419       Running ●
Size filter        318,200         Running ●
Partial hash       41,200          Running ●
Full hash          8,300           Running ●
DB write           7,900           Running ●

Files discovered:    1,243,419
Candidates found:      318,200    (25.6%)
Data read:               42 GB

Duplicate groups so far:    4,200
Reclaimable space so far:   18.4 GB

[Cancel scan]
```

This transparency is borrowed from Gemini 2's real-time scan feedback and directly uses the §5.6 progress counters already planned in the requirements. Users with NAS hardware will want to see that the slow stage is the full hash pool (disk I/O limited), not a software bug.

### 3.7 Trash View

Match the pattern of "time remaining before auto-purge" — users who made a mistake two days ago can still recover.

```
[File]               [Original path]           [Size]   [Expires]   [Restore]
IMG_0042.jpg         /volume1/photos/2023/      5.2 MB   28 days     [↩ Restore]
vacation_video.mp4   /volume1/videos/trips/     2.1 GB   27 days     [↩ Restore]

[Purge all now]  (destructive — requires confirmation dialog)
```

Color-code the expiry: green for > 14 days, yellow for 7–14 days, red for < 7 days.

### 3.8 Tailwind CSS Implementation Notes

**Color system:**
- Use a neutral base (gray-900 background for dark mode, white for light)
- Green (`green-500` / `green-600`) for "keep" / safe actions
- Red (`red-500` / `red-600`) for "delete" / destructive actions
- Amber/orange (`amber-500`) for delta values and warnings
- Blue (`blue-500`) for navigation and informational elements

**Component patterns:**
- File type badges: `rounded-full px-2 py-0.5 text-xs font-medium` with category-specific colors
- Stat cards on dashboard: `rounded-2xl bg-white shadow-sm p-6` with a large number and small label
- Progress bars: native HTML `<progress>` styled with Tailwind, or a simple `div` with `bg-green-500 h-2 rounded-full transition-all`
- Skeleton loading for thumbnails: `animate-pulse bg-gray-200 rounded` placeholder while images load
- Confirmation dialogs: use a simple `<dialog>` element or a Tailwind-styled overlay modal — avoid browser `confirm()` which cannot be styled

**HTMX patterns for the Go/HTMX stack:**
- Poll the scan progress endpoint every 2 seconds using `hx-trigger="every 2s"` on the progress widget
- Use `hx-swap="outerHTML"` on the group detail card after a deletion, replacing the confirmed group with a "Next group →" prompt
- Use `hx-push-url` to update the browser URL when navigating between groups — enables back/forward navigation and shareable links to specific groups

---

## 4. Summary Table

| Concern | Recommended pattern |
|---|---|
| Group list default sort | Reclaimable space descending |
| Group row layout | Thumbnail + name + "N copies · X MB reclaimable" |
| Group detail layout | Horizontal card grid, one card per file copy |
| Keep/delete selection | Radio button "KEEP" (implicit delete for all others) |
| Image comparison | Side-by-side cards; optional full-screen slider modal for 2 |
| Video preview | Poster frame in card; click to stream short clip |
| Delta metadata | Show size difference in amber if copy is not the largest |
| Auto-selection | "Auto-select oldest" / "Keep files in [path]" bulk rules |
| Safety | Reference file protected via radio (not checkbox-per-delete) |
| Bulk actions | Toolbar appears on multi-group checkbox selection |
| Scan progress | Per-stage breakdown matching pipeline stages |
| Session progress | "N / M groups resolved · X GB freed" persistent header |
| Soft deletion | Trash with 30-day TTL, color-coded expiry, restore per file |
| Keyboard shortcuts | Arrow keys, number keys, D to confirm |

---

## Sources

- [dupeGuru documentation — Results](https://dupeguru.voltaicideas.net/help/en/results.html)
- [dupeGuru GitHub](https://github.com/arsenetar/dupeguru)
- [dupeGuru guide — rapidseedbox](https://www.rapidseedbox.com/blog/dupeguru-guide)
- [Duplicate Cleaner Pro — Duplicate Files tab manual](https://www.duplicatecleaner.com/manual4/duplicate_files_tab.htm)
- [Duplicate Cleaner Pro feature list](https://www.digitalvolcano.co.uk/dcfeatures.html)
- [Duplicate Cleaner Pro tutorial — PictureEcho](https://pictureecho.com/blog/duplicate-cleaner-pro-5-tutorial/)
- [Gemini 2 product page — MacPaw](https://macpaw.com/gemini)
- [Gemini 2 review — Macworld](https://www.macworld.com/article/2434914/gemini-2-duplicate-file-finder-review.html)
- [Gemini 2 analysis — TechJury](https://techjury.net/product-analysis/gemini-2/)
- [AllDup help](https://www.alldup.de/alldup_help/alldup.php)
- [AllDup review — cisdem](https://www.cisdem.com/resource/alldup-duplicate-file-finder.html)
- [Czkawka GitHub](https://github.com/qarmin/czkawka)
- [Czkawka 7.0 / Krokiet — Medium](https://medium.com/@qarmin/czkawka-7-0-a465036e8788)
- [fclones-gui GitHub](https://github.com/pkolaczk/fclones-gui)
- [fclones-gui — Hacker News](https://news.ycombinator.com/item?id=36494327)
- [dude (Duplicates Detector) GitHub](https://github.com/PJDude/dude)
- [immich_duplicate_finder GitHub](https://github.com/vale46n1/immich_duplicate_finder)
- [immich-deduper GitHub](https://github.com/RazgrizHsu/immich-deduper)
- [Immich duplicate detection discussion](https://github.com/immich-app/immich/discussions/1968)
