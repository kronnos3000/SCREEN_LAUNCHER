# ScreenLauncher

[![GitHub](https://img.shields.io/github/v/release/kronnos3000/SCREEN_LAUNCHER?include_prereleases&label=release)](https://github.com/kronnos3000/SCREEN_LAUNCHER/releases)

One-click Windows launcher that opens Chrome windows on specific monitors.

- **Weekly Gantt meeting**: Google Meet on the left screen, and the JobTread **PRODUCTION** view's jobs on the right screen, one tab per job on its Schedule page. Buttons: **1st half / 2nd half / All**.
- The view's filter and sort are read from JobTread when you click, so edits to the view carry over. Read-only: the app never writes to JobTread.
- Key: `JOBTREAD_GRANT_KEY` environment variable, or the `jobtread.keyFile` .env path in the presets file.
- Presets: `ScreenLauncher-presets.json` next to the exe (created on first run; "Edit presets..." opens it). Monitors: `main`, `second`, `third`, `left`, `right`. A window can have `urls` (many tabs in one Chrome call).

Build: `go build -ldflags "-H windowsgui -s -w" -o ScreenLauncher.exe .`
