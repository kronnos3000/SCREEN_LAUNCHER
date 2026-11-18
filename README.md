# ScreenLauncher

[![GitHub](https://img.shields.io/github/v/release/kronnos3000/SCREEN_LAUNCHER?include_prereleases&label=release)](https://github.com/kronnos3000/SCREEN_LAUNCHER/releases)

One-click Windows launcher that opens Chrome windows on specific monitors. Auto-updates from GitHub releases.

## Variants

| Variant | File | Screens | Purpose |
|---------|------|---------|---------|
| **CR2** | `ScreenLauncher-CR2.exe` | Dual | Conference Room 2: Weekly Gantt meeting (Meet + JobTread jobs) |
| **SD** | `ScreenLauncher-SD.exe` | Single | Sales Display: Sales Pipeline views (Meet minimized for recording) |

## CR2 — Conference Room 2

- **Weekly Gantt meeting**: Google Meet on the left screen, and the JobTread **PRODUCTION** view's jobs on the right screen, one tab per job on its Schedule page.
- Buttons: **1st half / 2nd half / All**
- Additional meetings: In-office, Company-wide, Field team

## SD — Sales Display

- Opens Google Meet **minimized** (for recording)
- Opens **SALES PIPELINE (List View)** and **SALES PIPELINE (Kanban)** on a single screen
- One-button launch

## Configuration

- **JobTread Key**: `JOBTREAD_GRANT_KEY` environment variable, or via Preferences dialog
- **Presets**: `ScreenLauncher-presets.json` next to the exe (created on first run)
- **Auto-update**: Checks GitHub releases on startup, downloads and applies updates automatically

## Build

```powershell
# Build both variants
.\build.ps1 -Version "1.0.0"

# Build specific variant
.\build.ps1 -Version "1.0.0" -Variant SD

# Dev build (no version, no auto-update)
.\build.ps1
```

Requires Go 1.24+. Output: `ScreenLauncher-CR2.exe`, `ScreenLauncher-SD.exe`
