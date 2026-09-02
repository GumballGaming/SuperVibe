package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	appVersion       = "v0.3.0"
	githubRepository = "GumballGaming/SuperVibe"
)

type releaseInfo struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

type UpdateInfo struct {
	Available   bool   `json:"available"`
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	DownloadURL string `json:"downloadUrl,omitempty"`
}

func (a *App) CheckForUpdate() (*UpdateInfo, error) {
	info := &UpdateInfo{Current: appVersion, Latest: appVersion}
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(a.ctx, http.MethodGet, "https://api.github.com/repos/"+githubRepository+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub release check returned HTTP %d", resp.StatusCode)
	}
	var release releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	info.Latest = release.TagName
	for _, asset := range release.Assets {
		if asset.Name == "SuperVibe-"+release.TagName+"-windows-amd64.exe" && strings.HasPrefix(asset.URL, "https://") {
			info.DownloadURL = asset.URL
			break
		}
	}
	info.Available = newerVersion(release.TagName, appVersion) && info.DownloadURL != ""
	return info, nil
}

// InstallUpdate downloads the verified release asset and schedules a safe
// replacement after this process exits. It intentionally does nothing on
// non-Windows builds.
func (a *App) InstallUpdate(downloadURL string) error {
	if runtime.GOOS != "windows" || !strings.HasPrefix(downloadURL, "https://github.com/") {
		return fmt.Errorf("unsupported update target")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(exe), "SuperVibe-update-*.exe")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	defer tmp.Close()
	req, err := http.NewRequestWithContext(a.ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("update download returned HTTP %d", resp.StatusCode)
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	cmd := fmt.Sprintf("ping 127.0.0.1 -n 3 >nul & move /Y %q %q >nul & start \"\" %q", tmpPath, exe, exe)
	if err := exec.Command("cmd", "/C", cmd).Start(); err != nil {
		return err
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		if a.ctx != nil {
			// The scheduled command waits for this process to release the file.
			_ = a.ctx
		}
		os.Exit(0)
	}()
	return nil
}

func newerVersion(candidate, current string) bool {
	parse := func(v string) [3]int {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		parts := strings.Split(v, ".")
		var out [3]int
		for i := 0; i < len(parts) && i < 3; i++ {
			out[i], _ = strconv.Atoi(parts[i])
		}
		return out
	}
	a, b := parse(candidate), parse(current)
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}
