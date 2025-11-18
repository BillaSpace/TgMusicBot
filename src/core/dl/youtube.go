/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package dl

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"ashokshau/tgmusic/src/config"
	"ashokshau/tgmusic/src/core/cache"
)

// YouTubeData provides an interface for fetching track and playlist information from YouTube.
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

// NewYouTubeData initializes a YouTubeData instance with pre-compiled regex patterns and a cleaned query.
func NewYouTubeData(query string) *YouTubeData {
	return &YouTubeData{
		Query:    clearQuery(query),
		ApiUrl:   strings.TrimRight(config.Conf.ApiUrl, "/"),
		APIKey:   config.Conf.ApiKey,
		Patterns: youtubePatterns,
	}
}

// clearQuery removes extraneous URL parameters and fragments from a given query string.
func clearQuery(query string) string {
	query = strings.SplitN(query, "#", 2)[0]
	query = strings.SplitN(query, "&", 2)[0]
	return strings.TrimSpace(query)
}

// normalizeYouTubeURL converts various YouTube URL formats (e.g., youtu.be, shorts) into a standard watch URL.
func (y *YouTubeData) normalizeYouTubeURL(urlStr string) string {
	var videoID string
	switch {
	case strings.Contains(urlStr, "youtu.be/"):
		parts := strings.SplitN(strings.SplitN(urlStr, "youtu.be/", 2)[1], "?", 2)
		videoID = strings.SplitN(parts[0], "#", 2)[0]
	case strings.Contains(urlStr, "youtube.com/shorts/"):
		parts := strings.SplitN(strings.SplitN(urlStr, "youtube.com/shorts/", 2)[1], "?", 2)
		videoID = strings.SplitN(parts[0], "#", 2)[0]
	default:
		return urlStr
	}
	return "https://www.youtube.com/watch?v=" + videoID
}

// extractVideoID parses a YouTube URL and extracts the video ID.
func (y *YouTubeData) extractVideoID(urlStr string) string {
	urlStr = y.normalizeYouTubeURL(urlStr)
	for _, pattern := range y.Patterns {
		if match := pattern.FindStringSubmatch(urlStr); len(match) > 1 {
			return match[1]
		}
	}
	return ""
}

// IsValid checks if the query string matches any of the known YouTube URL patterns.
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

// GetInfo retrieves metadata for a track from YouTube.
// It returns a PlatformTracks object or an error if the information cannot be fetched.
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

// Search performs a search for a track on YouTube.
// It accepts a context for handling timeouts and cancellations, and returns a PlatformTracks object or an error.
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

// GetTrack retrieves detailed information for a single track.
// It returns a TrackInfo object or an error if the track cannot be found.
func (y *YouTubeData) GetTrack(ctx context.Context) (cache.TrackInfo, error) {
	if y.Query == "" {
		return cache.TrackInfo{}, errors.New("the query is empty")
	}
	if !y.IsValid() {
		return cache.TrackInfo{}, errors.New("the provided URL is invalid or the platform is not supported")
	}

	// keep previous behavior: attempt Api GetTrack only if both ApiUrl and APIKey are present,
	// otherwise fallback to local GetInfo
	if y.ApiUrl != "" && y.APIKey != "" {
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

// downloadTrack handles the download of a track from YouTube.
// It returns the file path of the downloaded track or an error if the download fails.
func (y *YouTubeData) downloadTrack(ctx context.Context, info cache.TrackInfo, video bool) (string, error) {
	// New behavior: if API base is configured, always try API first (API key optional),
	// then fall back to yt-dlp if API call fails for any reason.
	if y.ApiUrl != "" {
		if filePath, err := y.downloadWithApi(ctx, info.TC, video); err == nil {
			return filePath, nil
		} else {
			// log error and fall back to yt-dlp
			log.Printf("downloadWithApi failed for %s: %v — falling back to yt-dlp", info.TC, err)
		}
	}

	// Fallback to yt-dlp
	filePath, err := y.downloadWithYtDlp(ctx, info.TC, video)
	return filePath, err
}

// BuildYtdlpParams constructs the command-line parameters for yt-dlp to download media.
// It takes a video ID and a boolean indicating whether to download video or audio, and returns the corresponding parameters.
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

// downloadWithYtDlp downloads media from YouTube using the yt-dlp command-line tool.
// It returns the file path of the downloaded track or an error if the download fails.
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

		return "", fmt.Errorf("an unexpected error occurred while downloading %s: %w", videoID, err)
	}

	downloadedPathStr := strings.TrimSpace(string(output))
	if downloadedPathStr == "" {
		return "", fmt.Errorf("no output path was returned for %s", videoID)
	}

	if _, err := os.Stat(downloadedPathStr); os.IsNotExist(err) {
		return "", fmt.Errorf("the file was not found at the reported path: %s", downloadedPathStr)
	}

	return downloadedPathStr, nil
}

// getCookieFile retrieves the path to a cookie file from the configured list.
// It returns the path to a randomly selected cookie file.
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

// downloadWithApi downloads a track using the external API.
// It returns the file path of the downloaded track or an error if the download fails.
func (y *YouTubeData) downloadWithApi(ctx context.Context, videoID string, _ bool) (string, error) {
	// Build the stream URL using the configured api base and the original youtube URL (video id)
	if y.ApiUrl == "" {
		return "", errors.New("api url is not configured")
	}

	// Build provided example: {ApiUrl}/stream?url=https://youtu.be/{id}
	streamBase := strings.TrimRight(y.ApiUrl, "/") + "/stream"
	values := url.Values{}
	videoURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID)
	values.Set("url", videoURL)
	streamURL := fmt.Sprintf("%s?%s", streamBase, values.Encode())

	headers := map[string]string{}
	if y.APIKey != "" {
		headers["X-API-Key"] = y.APIKey
	}

	resp, err := sendRequest(ctx, http.MethodGet, streamURL, nil, headers)
	if err != nil {
		return "", err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code from api stream: %s", resp.Status)
	}

	// Choose extension mapping: audio -> .m4a (even if API returns .oga), video -> .mp4
	ct := resp.Header.Get("Content-Type")
	ext := ".m4a"
	if strings.HasPrefix(ct, "video/") {
		ext = ".mp4"
	} else {
		// check content-disposition for filename ext
		if cd := resp.Header.Get("Content-Disposition"); cd != "" {
			if _, params, perr := mime.ParseMediaType(cd); perr == nil {
				if fn, ok := params["filename"]; ok {
					e := filepath.Ext(fn)
					if e != "" {
						// normalize oga/ogg to .m4a as requested
						if strings.EqualFold(e, ".oga") || strings.EqualFold(e, ".ogg") {
							ext = ".m4a"
						} else {
							ext = e
						}
					}
				}
			}
		}
	}

	targetPath := filepath.Join(config.Conf.DownloadsDir, videoID+ext)
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return "", fmt.Errorf("failed to create downloads dir: %w", err)
	}

	tmp := targetPath + ".part"
	out, oerr := os.Create(tmp)
	if oerr != nil {
		return "", fmt.Errorf("failed to create temp file: %w", oerr)
	}
	_, copyErr := io.Copy(out, resp.Body)
	_ = out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("failed to save stream: %w", copyErr)
	}

	if err := os.Rename(tmp, targetPath); err != nil {
		return "", fmt.Errorf("failed to rename temp file: %w", err)
	}

	return targetPath, nil
}
