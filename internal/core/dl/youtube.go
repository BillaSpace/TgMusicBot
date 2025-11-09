/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025 Ashok Shau
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package dl

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/AshokShau/TgMusicBot/internal/config"
	"github.com/AshokShau/TgMusicBot/internal/core/cache"
)

type YouTubeData struct {
	Query    string
	ApiUrl   string
	APIKey   string
	Patterns map[string]*regexp.Regexp
}

var youtubePatterns = map[string]*regexp.Regexp{
	"youtube":   regexp.MustCompile(`^(?:https?://)?(?:www\.)?youtube\.com/watch\?v=([\w-]{11})(?:[&#?].*)?$`),
	"youtu_be":  regexp.MustCompile(`^(?:https?://)?(?:www\.)?youtu\.be/([\w-]{11})(?:[?#].*)?$`),
	"yt_shorts": regexp.MustCompile(`^(?:https?://)?(?:www\.)?youtube\.com/shorts/([\w-]{11})(?:[?#].*)?$`),
}

func NewYouTubeData(query string) *YouTubeData {
	return &YouTubeData{
		Query:    clearQuery(query),
		ApiUrl:   strings.TrimRight(config.Conf.ApiUrl, "/"),
		APIKey:   "",
		Patterns: youtubePatterns,
	}
}

func clearQuery(query string) string {
	query = strings.SplitN(query, "#", 2)[0]
	query = strings.SplitN(query, "&", 2)[0]
	return strings.TrimSpace(query)
}

func (y *YouTubeData) normalizeYouTubeURL(u string) string {
	var videoID string
	switch {
	case strings.Contains(u, "youtu.be/"):
		parts := strings.SplitN(strings.SplitN(u, "youtu.be/", 2)[1], "?", 2)
		videoID = strings.SplitN(parts[0], "#", 2)[0]
	case strings.Contains(u, "youtube.com/shorts/"):
		parts := strings.SplitN(strings.SplitN(u, "youtube.com/shorts/", 2)[1], "?", 2)
		videoID = strings.SplitN(parts[0], "#", 2)[0]
	default:
		return u
	}
	return "https://www.youtube.com/watch?v=" + videoID
}

func (y *YouTubeData) extractVideoID(u string) string {
	u = y.normalizeYouTubeURL(u)
	for _, pattern := range y.Patterns {
		if match := pattern.FindStringSubmatch(u); len(match) > 1 {
			return match[1]
		}
	}
	return ""
}

func (y *YouTubeData) IsValid() bool {
	if y.Query == "" {
		log.Println("The query or patterns are empty.")
		return false
	}
	for _, pattern := range y.Patterns {
		if pattern.MatchString(y.Query) {
			return true
		}
	}
	return false
}

func (y *YouTubeData) GetInfo(ctx context.Context) (cache.PlatformTracks, error) {
	if !y.IsValid() {
		return cache.PlatformTracks{}, errors.New("the provided URL is invalid or the platform is not supported")
	}

	y.Query = y.normalizeYouTubeURL(y.Query)
	videoID := y.extractVideoID(y.Query)
	if videoID == "" {
		return cache.PlatformTracks{}, errors.New("unable to extract the video ID")
	}

	tracks, err := searchYouTube(y.Query)
	if err != nil {
		return cache.PlatformTracks{}, err
	}

	for _, track := range tracks {
		if track.ID == videoID {
			return cache.PlatformTracks{Results: []cache.MusicTrack{track}}, nil
		}
	}

	return cache.PlatformTracks{}, errors.New("no video results were found")
}

func (y *YouTubeData) Search(ctx context.Context) (cache.PlatformTracks, error) {
	tracks, err := searchYouTube(y.Query)
	if err != nil {
		return cache.PlatformTracks{}, err
	}
	if len(tracks) == 0 {
		return cache.PlatformTracks{}, errors.New("no video results were found")
	}
	return cache.PlatformTracks{Results: tracks}, nil
}

func (y *YouTubeData) GetTrack(ctx context.Context) (cache.TrackInfo, error) {
	if y.Query == "" {
		return cache.TrackInfo{}, errors.New("the query is empty")
	}
	if !y.IsValid() {
		return cache.TrackInfo{}, errors.New("the provided URL is invalid or the platform is not supported")
	}

	if y.ApiUrl != "" {
		if trackInfo, err := NewApiData(y.Query).GetTrack(ctx); err == nil {
			return trackInfo, nil
		}
	}

	getInfo, err := y.GetInfo(ctx)
	if err != nil {
		return cache.TrackInfo{}, err
	}
	if len(getInfo.Results) == 0 {
		return cache.TrackInfo{}, errors.New("no video results were found")
	}

	track := getInfo.Results[0]
	trackInfo := cache.TrackInfo{
		URL:      track.URL,
		CdnURL:   "None",
		Key:      "None",
		Name:     track.Name,
		Duration: track.Duration,
		TC:       track.ID,
		Cover:    track.Cover,
		Platform: "youtube",
	}

	return trackInfo, nil
}

func (y *YouTubeData) BuildYtdlpParams(videoID string, video bool) []string {
	outputTemplate := filepath.Join(config.Conf.DownloadsDir, "%(id)s.%(ext)s")

	params := []string{
		"yt-dlp",
		"--no-warnings",
		"--quiet",
		"--geo-bypass",
		"--retries", "2",
		"--continue",
		"--no-part",
		"--concurrent-fragments", "3",
		"--socket-timeout", "10",
		"--throttled-rate", "100K",
		"--retry-sleep", "1",
		"--no-write-thumbnail",
		"--no-write-info-json",
		"--no-embed-metadata",
		"--no-embed-chapters",
		"--no-embed-subs",
		"--extractor-args", "youtube:player_js_version=actual",
		"-o", outputTemplate,
	}

	formatSelector := "bestaudio[ext=m4a]/bestaudio[ext=mp4]/bestaudio[ext=webm]/bestaudio/best"
	if video {
		formatSelector = "bestvideo[ext=mp4][height<=1080]+bestaudio[ext=m4a]/best[ext=mp4][height<=1080]"
		params = append(params, "--merge-output-format", "mp4")
	}
	params = append(params, "-f", formatSelector)

	if cookieFile := y.getCookieFile(); cookieFile != "" {
		params = append(params, "--cookies", cookieFile)
	} else if config.Conf.Proxy != "" {
		params = append(params, "--proxy", config.Conf.Proxy)
	}

	videoURL := "https://www.youtube.com/watch?v=" + videoID
	params = append(params, videoURL, "--print", "after_move:filepath")

	return params
}

func (y *YouTubeData) downloadWithYtDlp(ctx context.Context, videoID string, video bool) (string, error) {
	ytdlpParams := y.BuildYtdlpParams(videoID, video)
	cmd := exec.CommandContext(ctx, ytdlpParams[0], ytdlpParams[1:]...)

	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr := string(exitErr.Stderr)
			return "", fmt.Errorf("yt-dlp failed with exit code %d: %s", exitErr.ExitCode(), stderr)
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("yt-dlp timed out for video ID: %s", videoID)
		}
		return "", fmt.Errorf("unexpected yt-dlp error for %s: %w", videoID, err)
	}

	downloadedPathStr := strings.TrimSpace(string(output))
	if downloadedPathStr == "" {
		return "", fmt.Errorf("no output path was returned for %s", videoID)
	}
	if _, err := os.Stat(downloadedPathStr); os.IsNotExist(err) {
		return "", fmt.Errorf("file not found at reported path: %s", downloadedPathStr)
	}

	return downloadedPathStr, nil
}

func (y *YouTubeData) downloadTrack(ctx context.Context, info cache.TrackInfo, video bool) (string, error) {
	videoID := info.TC
	if videoID == "" {
		videoID = y.extractVideoID(info.URL)
	}
	if videoID == "" {
		return "", errors.New("missing YouTube video ID")
	}

	if base := strings.TrimRight(y.ApiUrl, "/"); base != "" {
		if video {
			watchURL := "https://www.youtube.com/watch?v=" + videoID
			apiURL := base + "/yt?" + url.Values{"url": {watchURL}}.Encode()

			resp, err := sendRequest(ctx, http.MethodGet, apiURL, nil, nil)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var data struct {
						Success     bool   `json:"success"`
						DownloadURL string `json:"download_url"`
						Credit      string `json:"credit"`
					}
					if json.NewDecoder(resp.Body).Decode(&data) == nil && data.Success && data.DownloadURL != "" {
						ext := pickExtFromURL(data.DownloadURL, ".mp4")
						target := filepath.Join(config.Conf.DownloadsDir, fmt.Sprintf("%s%s", videoID, ext))
						if path, derr := DownloadFile(ctx, data.DownloadURL, target, false); derr == nil {
							return path, nil
						}
					}
				}
			}
		} else {
			apiURL := base + "/yt?" + url.Values{
				"id":     {videoID},
				"type":   {"audio"},
				"format": {"m4a"},
			}.Encode()

			resp, err := sendRequest(ctx, http.MethodGet, apiURL, nil, nil)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var data struct {
						Success     bool   `json:"success"`
						DownloadURL string `json:"download_url"`
						Credit      string `json:"credit"`
					}
					if json.NewDecoder(resp.Body).Decode(&data) == nil && data.Success && data.DownloadURL != "" {
						ext := pickExtFromURL(data.DownloadURL, ".m4a")
						target := filepath.Join(config.Conf.DownloadsDir, fmt.Sprintf("%s%s", videoID, ext))
						if path, derr := DownloadFile(ctx, data.DownloadURL, target, false); derr == nil {
							return path, nil
						}
					}
				}
			}
		}
	}

	return y.downloadWithYtDlp(ctx, videoID, video)
}

func (y *YouTubeData) getCookieFile() string {
	cookiesPath := config.Conf.CookiesPath
	if len(cookiesPath) == 0 {
		return ""
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(cookiesPath))))
	if err != nil {
		log.Printf("Could not generate a random number: %v", err)
		return cookiesPath[0]
	}
	return cookiesPath[n.Int64()]
}

func pickExtFromURL(u, fallback string) string {
	if parsed, err := url.Parse(u); err == nil {
		if ext := filepath.Ext(parsed.Path); ext != "" && len(ext) <= 6 {
			return ext
		}
	}
	return fallback
}
