# ScreenLauncher — Handoff for Claude in VS Code

**Owner:** Pablo Lorenzo (GitHub: `kronnos3000`), Construction Corps
**Repo:** https://github.com/kronnos3000/SCREEN_LAUNCHER (public)
**Target machine:** Windows 11 x64, multi-monitor (`sulaco-ccorps`); should also run on Pablo's other PCs
**Handoff date:** 2026-11-17

---

## 1. What this is

A small Windows desktop app. Pablo opens it, picks a preset, and it opens Chrome windows on specific monitors — the main use is **Google Meet on one screen and JobTread on the other**. It must be a real app with a window and buttons, **not** a shortcut, batch file, or script.

## 2. Current state

A working app with these files:
- `main.go` — main window, preset buttons, Chrome window management
- `jobtread.go` — JobTread API integration for fetching PRODUCTION view jobs
- `prefs.go` — Preferences dialog (form-based, no JSON editing)
- `dpapi.go` — Windows DPAPI for secure grant key storage
- `logo.go` — embedded app icon
- `installer/` — NSIS-based installer builder
- `assets/logo.png` — source logo

Builds and runs on Windows 11. The Weekly Gantt meeting preset works (1st half / 2nd half / All buttons open Meet + job schedule tabs).

Build command:

```powershell
go build -ldflags "-H windowsgui -s -w" -o ScreenLauncher.exe .
```

No third-party dependencies — standard library and raw Win32 calls only.

## 3. How the prototype works

1. Shows a small fixed-size window titled "Screen Launcher" with one button per preset, a status line, and an "Edit presets..." button.
2. On click, for each window in the preset:
   - Snapshots the existing Chrome top-level windows (class `Chrome_WidgetWin_1`, owned by `chrome.exe`).
   - Runs `chrome.exe --new-window <url>` (or `--app=<url>` if the preset says `"app": true`).
   - Polls up to 20 s for a new Chrome window, then restores it, moves it to the target monitor's work area, and maximizes it.
3. Closes itself when done (`closeAfterLaunch`).

Why move-after-launch instead of `--window-position`: Chrome ignores position flags when it is already running under the same profile, and Pablo needs his normal signed-in profile (Meet and JobTread logins).

Monitor naming: `main` = Windows primary display; `second`, `third`, `fourth` = the remaining displays ordered left to right. If a preset names a monitor that doesn't exist, it falls back to `main`.

Presets live in `ScreenLauncher-presets.json` next to the exe (auto-created on first run):

```json
{
  "browser": "",
  "closeAfterLaunch": true,
  "presets": [
    { "name": "Meet on main screen  +  JobTread on second",
      "windows": [
        { "url": "https://meet.google.com/", "monitor": "main" },
        { "url": "https://app.jobtread.com/", "monitor": "second" }
      ] }
  ]
}
```

Built-in presets: Meet main + JobTread second; JobTread main + Meet second; Meet only; JobTread only.

## 4. Completed setup

- [x] Git repo initialized and pushed to GitHub (public)
- [x] Go toolchain confirmed working
- [x] App builds and runs
- [x] App icon embedded (`logo.go` + `assets/logo.png`)
- [x] Preferences dialog (form-based, not JSON editing)
- [x] JobTread integration with secure key storage (DPAPI)
- [x] Weekly Gantt meeting preset with 1st half / 2nd half / All buttons
- [x] **Variant system** — CR2 (dual screen) and SD (single screen) built from same codebase
- [x] **Auto-updater** — checks GitHub releases, downloads correct variant, self-replaces
- [x] **SD variant** — Sales Display with SALES PIPELINE views (List + Kanban), Meet minimized

**Build both:** `.\build.ps1 -Version "1.0.0"`

### Test checklist

- [x] Window appears centered on the main screen, with modern-looking (themed) buttons and readable text at the display's scaling
- [x] Meet + JobTread preset with Chrome **already open** — each lands maximized on the right monitor
- [x] Same preset with Chrome **fully closed** first
- [x] Monitors with different scaling and negative coordinates handled
- [x] Only one monitor connected — everything falls back to the main screen
- [x] Preferences via form dialog (no JSON editing required)
- [ ] Installer tested on clean Windows machine

## 5. Known risks in the prototype

- **Window detection is a heuristic.** It picks the new Chrome window (preferring the foreground one). A cold Chrome start with session restore can create several new windows at once.
- **Visual styles** are enabled at runtime by borrowing the common-controls manifest from `shell32.dll` (resource 124). If buttons look like Windows 95, embed a proper manifest instead.
- **DPI awareness** is set by API call at startup rather than by manifest. Embedding a manifest (PerMonitorV2) is the more robust fix and solves the item above at the same time.
- **Unsigned exe** — SmartScreen will warn on first run on other PCs.
- Only Chrome is searched for (`Program Files`, `Program Files (x86)`, `%LOCALAPPDATA%`). The `browser` setting can override the path.

## 6. Decisions made

1. **Stay in Go** — decided 2026-11-17. ~3 MB exe, no runtime, exits after launching.
2. **Weekly Gantt meeting preset** — Meet on left, JobTread PRODUCTION view jobs on right (1st half / 2nd half / All).
3. **Normal Chrome window** (not chromeless app window).
4. **Live JobTread data** — yes, fetches jobs from PRODUCTION view via API.

## 7. Connecting to JobTread (JT)

There are three separate things "connect to JT" can mean. Only A is needed for the app as specified.

### A. The app opening JobTread (needed — no API)

The app just opens `https://app.jobtread.com/` in Pablo's normal Chrome profile, where he is already signed in. No keys, no API.

### B. Claude in VS Code talking to JobTread (for looking things up while developing)

JobTread hosts an official MCP server (the "AI Connector") at `https://api.jobtread.com/mcp`. Add it to Claude Code:

```powershell
claude mcp add --transport http jobtread https://api.jobtread.com/mcp
```

Then run `/mcp` inside Claude Code and complete the JobTread sign-in in the browser. Access follows Pablo's own JobTread permissions.

### C. The app (or any script) calling the JobTread API directly (only if decision 4 is "yes")

- **Endpoint:** `POST https://api.jobtread.com/pave`, `Content-Type: application/json`. One endpoint for everything; the query language is "Pave" (GraphQL-like).
- **Auth:** a grant key, passed inside the query body — not in a header.
- **Getting a key:** JobTread → Settings → Integrations → JobTread API → Grants (`https://app.jobtread.com/settings/integrations/api/grants`) → add a grant. The key starts with `grant_`, is shown **once**, and expires after about 3 months without use.
- **Storing it:** environment variable `JOBTREAD_GRANT_KEY` (or a local `.env`). **Never commit it and never paste it into chat.**

Connection test:

```powershell
$body = @{ query = @{ '$' = @{ grantKey = $env:JOBTREAD_GRANT_KEY }
                      currentGrant = @{ id = @{}; user = @{ name = @{} } } } } | ConvertTo-Json -Depth 6
Invoke-RestMethod -Uri https://api.jobtread.com/pave -Method Post -ContentType 'application/json' -Body $body
```

A reply with the grant id and Pablo's name means it works.

Exploring the schema (patterns that worked in earlier Construction Corps sessions):

```json
{ "schema": { "$": { "path": "root", "search": "job" } } }
{ "schema": { "$": { "expand": true, "path": "root.createComment.$" } } }
```

Useful facts from earlier work:

- Files are queried at the organization level (`organization.files`, filtered by `job.id`), not under a job.
- One property name often maps to several job records — match on job number, not name.
- `createComment` posts and notifies immediately; there is no draft state.

**House rules for JobTread:**

- Read freely. **Do not write to JobTread (create, update, comment, upload) without Pablo's explicit approval of the exact change.**
- Use a dedicated grant for this project so it can be revoked on its own.

`.gitignore` minimum:

```
*.exe
*.syso
.env
ScreenLauncher-presets.json
```

A possible JT-powered feature if Pablo wants it: a preset or search box that opens a specific job's page directly on the JobTread screen. Verify the job-page URL format in the browser before building it.

## 8. Working preferences

- Keep answers short; Pablo asks when he wants detail.
- Automate as much as possible; only bring him the important decisions (section 6).
- He is left-handed with swapped mouse buttons on `sulaco-ccorps` — irrelevant to the app, relevant if anything ever simulates clicks.
