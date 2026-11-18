//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	repoOwner = "kronnos3000"
	repoName  = "SCREEN_LAUNCHER"
)

// GitHubRelease represents a GitHub release.
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// checkForUpdate checks GitHub for a newer version and returns download URL if available.
func checkForUpdate() (newVersion, downloadURL string, err error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", repoOwner, repoName)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", "", fmt.Errorf("could not check for updates: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("GitHub returned %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", fmt.Errorf("could not parse release info: %v", err)
	}

	// Compare versions (strip 'v' prefix)
	latestVer := strings.TrimPrefix(release.TagName, "v")
	currentVer := strings.TrimPrefix(Version, "v")

	if !isNewerVersion(latestVer, currentVer) {
		return "", "", nil // already up to date
	}

	// Find the asset for our variant
	assetName := fmt.Sprintf("ScreenLauncher-%s.exe", Variant)
	for _, asset := range release.Assets {
		if strings.EqualFold(asset.Name, assetName) {
			return release.TagName, asset.BrowserDownloadURL, nil
		}
	}

	return "", "", fmt.Errorf("no asset found for variant %s", Variant)
}

// isNewerVersion returns true if 'a' is newer than 'b' (simple semver comparison).
func isNewerVersion(a, b string) bool {
	if b == "dev" || b == "" {
		return a != "dev" && a != ""
	}
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		var ai, bi int
		fmt.Sscanf(aParts[i], "%d", &ai)
		fmt.Sscanf(bParts[i], "%d", &bi)
		if ai > bi {
			return true
		}
		if ai < bi {
			return false
		}
	}
	return len(aParts) > len(bParts)
}

// downloadUpdate downloads the new version to a temp file.
func downloadUpdate(url string) (string, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("download failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download returned %d", resp.StatusCode)
	}

	// Save to temp file next to the exe
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	tmpFile := filepath.Join(dir, fmt.Sprintf("ScreenLauncher-%s.update.exe", Variant))

	f, err := os.Create(tmpFile)
	if err != nil {
		return "", fmt.Errorf("could not create update file: %v", err)
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		os.Remove(tmpFile)
		return "", fmt.Errorf("download incomplete: %v", err)
	}

	return tmpFile, nil
}

// applyUpdate replaces the current exe with the new one using a helper script.
// The script waits for this process to exit, then replaces the exe and restarts.
func applyUpdate(newExePath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	// Create a batch script that:
	// 1. Waits for this process to exit
	// 2. Deletes the old exe (or renames it)
	// 3. Renames the new exe to the old name
	// 4. Starts the new exe
	// 5. Deletes itself

	scriptPath := filepath.Join(filepath.Dir(exe), "update.bat")
	script := fmt.Sprintf(`@echo off
setlocal
set "OLD=%s"
set "NEW=%s"
set "PID=%d"

:waitloop
tasklist /FI "PID eq %%PID%%" 2>NUL | find /I "%%PID%%" >NUL
if not errorlevel 1 (
    timeout /t 1 /nobreak >NUL
    goto waitloop
)

del "%%OLD%%" >NUL 2>&1
if exist "%%OLD%%" (
    move /Y "%%OLD%%" "%%OLD%%.old" >NUL 2>&1
)
move /Y "%%NEW%%" "%%OLD%%" >NUL 2>&1
start "" "%%OLD%%"
del "%%~f0" >NUL 2>&1
`, exe, newExePath, os.Getpid())

	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		return fmt.Errorf("could not create update script: %v", err)
	}

	// Start the script hidden
	cmd := exec.Command("cmd.exe", "/C", scriptPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		os.Remove(scriptPath)
		return fmt.Errorf("could not start update script: %v", err)
	}

	return nil
}

// doUpdateCheck runs in background, checks for update, and notifies user.
func doUpdateCheck(statusFn func(string)) {
	newVer, url, err := checkForUpdate()
	if err != nil {
		logf("update check error: %v", err)
		return
	}
	if url == "" {
		logf("already up to date (v%s)", Version)
		return
	}

	logf("update available: %s", newVer)
	statusFn(fmt.Sprintf("Update available: %s. Downloading...", newVer))

	tmpPath, err := downloadUpdate(url)
	if err != nil {
		logf("download error: %v", err)
		statusFn("Update download failed.")
		return
	}

	statusFn(fmt.Sprintf("Installing %s...", newVer))
	if err := applyUpdate(tmpPath); err != nil {
		logf("apply error: %v", err)
		statusFn("Update install failed.")
		os.Remove(tmpPath)
		return
	}

	// Exit so the script can replace us
	os.Exit(0)
}
