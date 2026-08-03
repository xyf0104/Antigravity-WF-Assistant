// Package updater performs a deliberately narrow, verified update check for
// Antigravity WF助手's own public GitHub releases. It never accepts an
// arbitrary URL from the renderer and verifies the published SHA256 manifest
// before handing an installer to the operating system.
package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	Repository     = "xyf0104/Antigravity-WF-Assistant"
	CurrentVersion = "1.4.0"
	maxAssetBytes  = int64(2 << 30) // installers are normally tens of MB
)

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	HTMLURL      string        `json:"html_url"`
	Body         string        `json:"body"`
	PublishedAt  string        `json:"published_at"`
	Prerelease   bool          `json:"prerelease"`
	Draft        bool          `json:"draft"`
	Assets       []githubAsset `json:"assets"`
}

// Info is renderer-safe metadata. AssetURL is intentionally omitted; the
// install method resolves the asset from GitHub again before downloading it.
type Info struct {
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	Available      bool   `json:"available"`
	Skipped        bool   `json:"skipped"`
	ReleaseURL     string `json:"releaseUrl"`
	AssetName      string `json:"assetName"`
	AssetSize      int64  `json:"assetSize"`
	PublishedAt    string `json:"publishedAt"`
	Notes          string `json:"notes"`
}

func Check(ctx context.Context, skippedVersion string) (Info, error) {
	release, err := latestRelease(ctx)
	if err != nil {
		return Info{CurrentVersion: CurrentVersion}, err
	}
	latest := normalizeVersion(release.TagName)
	if latest == "" {
		return Info{CurrentVersion: CurrentVersion}, fmt.Errorf("更新源返回了无效版本号")
	}
	asset, err := selectInstaller(release, runtime.GOOS)
	if err != nil {
		return Info{CurrentVersion: CurrentVersion, LatestVersion: latest, ReleaseURL: release.HTMLURL}, err
	}
	available := compareVersions(latest, CurrentVersion) > 0
	return Info{
		CurrentVersion: CurrentVersion,
		LatestVersion:  latest,
		Available:      available,
		Skipped:        available && normalizeVersion(skippedVersion) == latest,
		ReleaseURL:     release.HTMLURL,
		AssetName:      asset.Name,
		AssetSize:      asset.Size,
		PublishedAt:    release.PublishedAt,
		Notes:          truncateNotes(release.Body),
	}, nil
}

// DownloadLatestInstaller downloads the newest matching installer only after
// reconfirming it is newer than this build and validating its SHA256 against
// the release manifest. report may be nil.
func DownloadLatestInstaller(ctx context.Context, report func(downloaded, total int64)) (string, Info, error) {
	release, err := latestRelease(ctx)
	if err != nil {
		return "", Info{CurrentVersion: CurrentVersion}, err
	}
	latest := normalizeVersion(release.TagName)
	if compareVersions(latest, CurrentVersion) <= 0 {
		return "", Info{CurrentVersion: CurrentVersion, LatestVersion: latest}, fmt.Errorf("当前已是最新版本")
	}
	asset, err := selectInstaller(release, runtime.GOOS)
	if err != nil {
		return "", Info{CurrentVersion: CurrentVersion, LatestVersion: latest, ReleaseURL: release.HTMLURL}, err
	}
	if asset.Size <= 0 || asset.Size > maxAssetBytes {
		return "", Info{}, fmt.Errorf("更新安装包大小异常")
	}
	expected, err := releaseChecksum(ctx, release, asset.Name)
	if err != nil {
		return "", Info{}, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return "", Info{}, fmt.Errorf("无法创建安装包下载请求")
	}
	request.Header.Set("User-Agent", "Antigravity-WF-Assistant/"+CurrentVersion)
	response, err := httpClient().Do(request)
	if err != nil {
		return "", Info{}, fmt.Errorf("下载更新失败")
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", Info{}, fmt.Errorf("下载更新失败（HTTP %d）", response.StatusCode)
	}
	if response.ContentLength > maxAssetBytes {
		return "", Info{}, fmt.Errorf("更新安装包过大，已拒绝下载")
	}

	temp, err := os.CreateTemp("", "antigravity-wf-update-*."+installerExtension(asset.Name))
	if err != nil {
		return "", Info{}, err
	}
	path := temp.Name()
	success := false
	defer func() {
		if !success {
			_ = temp.Close()
			_ = os.Remove(path)
		}
	}()

	hash := sha256.New()
	reader := io.LimitReader(response.Body, maxAssetBytes+1)
	buffer := make([]byte, 128*1024)
	var downloaded int64
	for {
		read, readErr := reader.Read(buffer)
		if read > 0 {
			downloaded += int64(read)
			if downloaded > maxAssetBytes {
				return "", Info{}, fmt.Errorf("更新安装包过大，已拒绝下载")
			}
			if _, err := hash.Write(buffer[:read]); err != nil {
				return "", Info{}, err
			}
			if _, err := temp.Write(buffer[:read]); err != nil {
				return "", Info{}, err
			}
			if report != nil {
				report(downloaded, asset.Size)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", Info{}, fmt.Errorf("读取更新安装包失败")
		}
	}
	if asset.Size > 0 && downloaded != asset.Size {
		return "", Info{}, fmt.Errorf("更新安装包大小校验失败")
	}
	if got := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(got, expected) {
		return "", Info{}, fmt.Errorf("更新安装包校验失败，已丢弃下载文件")
	}
	if err := temp.Close(); err != nil {
		return "", Info{}, err
	}
	success = true
	return path, Info{
		CurrentVersion: CurrentVersion, LatestVersion: latest, Available: true,
		ReleaseURL: release.HTMLURL, AssetName: asset.Name, AssetSize: asset.Size,
		PublishedAt: release.PublishedAt, Notes: truncateNotes(release.Body),
	}, nil
}

func latestRelease(ctx context.Context) (githubRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+Repository+"/releases/latest", nil)
	if err != nil {
		return githubRelease{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "Antigravity-WF-Assistant/"+CurrentVersion)
	response, err := httpClient().Do(request)
	if err != nil {
		return githubRelease{}, fmt.Errorf("无法连接 GitHub 更新服务")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("检查更新失败（HTTP %d）", response.StatusCode)
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&release); err != nil {
		return githubRelease{}, fmt.Errorf("更新信息解析失败")
	}
	if release.Draft || release.Prerelease {
		return githubRelease{}, fmt.Errorf("没有可用的稳定版更新")
	}
	return release, nil
}

func releaseChecksum(ctx context.Context, release githubRelease, assetName string) (string, error) {
	var manifest *githubAsset
	for i := range release.Assets {
		if strings.EqualFold(release.Assets[i].Name, "SHA256SUMS.txt") {
			manifest = &release.Assets[i]
			break
		}
	}
	if manifest == nil {
		return "", fmt.Errorf("该版本未提供 SHA256SUMS.txt，已拒绝自动安装")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, manifest.BrowserDownloadURL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "Antigravity-WF-Assistant/"+CurrentVersion)
	response, err := httpClient().Do(request)
	if err != nil {
		return "", fmt.Errorf("无法下载更新校验清单")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("无法下载更新校验清单（HTTP %d）", response.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("读取更新校验清单失败")
	}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if name != assetName {
			continue
		}
		if len(fields[0]) == 64 {
			if _, err := hex.DecodeString(fields[0]); err == nil {
				return fields[0], nil
			}
		}
	}
	return "", fmt.Errorf("校验清单中缺少 %s", assetName)
}

func selectInstaller(release githubRelease, platform string) (githubAsset, error) {
	assets := append([]githubAsset(nil), release.Assets...)
	sort.Slice(assets, func(i, j int) bool { return assets[i].Name < assets[j].Name })
	for _, asset := range assets {
		name := strings.ToLower(asset.Name)
		switch platform {
		case "darwin":
			if strings.HasSuffix(name, ".pkg") && strings.Contains(name, "macos") {
				return asset, nil
			}
		case "windows":
			if strings.HasSuffix(name, ".exe") && strings.Contains(name, "windows") && strings.Contains(name, "setup") {
				return asset, nil
			}
		}
	}
	return githubAsset{}, fmt.Errorf("该版本没有适用于当前系统的安装包")
}

func normalizeVersion(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(value), "v"))
	if value == "" {
		return ""
	}
	for _, piece := range strings.Split(strings.SplitN(value, "-", 2)[0], ".") {
		if piece == "" {
			return ""
		}
		if _, err := strconv.Atoi(piece); err != nil {
			return ""
		}
	}
	return value
}

func compareVersions(left, right string) int {
	left, right = normalizeVersion(left), normalizeVersion(right)
	if left == "" || right == "" {
		return 0
	}
	leftParts, rightParts := strings.Split(left, "."), strings.Split(right, ".")
	length := len(leftParts)
	if len(rightParts) > length {
		length = len(rightParts)
	}
	for i := 0; i < length; i++ {
		var l, r int
		if i < len(leftParts) {
			l, _ = strconv.Atoi(leftParts[i])
		}
		if i < len(rightParts) {
			r, _ = strconv.Atoi(rightParts[i])
		}
		if l > r {
			return 1
		}
		if l < r {
			return -1
		}
	}
	return 0
}

func installerExtension(name string) string {
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	if extension == "pkg" || extension == "exe" {
		return extension
	}
	return "bin"
}

func truncateNotes(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > 6000 {
		return string(runes[:6000]) + "…"
	}
	return value
}

func httpClient() *http.Client {
	return &http.Client{Timeout: 45 * time.Second}
}
