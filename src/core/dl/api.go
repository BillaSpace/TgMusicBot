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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"log"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"ashokshau/tgmusic/src/config"
	"ashokshau/tgmusic/src/core/cache"
)

// ApiData provides a unified interface for fetching track and playlist information from various music platforms via an API gateway.
type ApiData struct {
	Query    string
	ApiUrl   string
	APIKey   string
	Patterns map[string]*regexp.Regexp
}

var apiPatterns = map[string]*regexp.Regexp{
	"apple_music": regexp.MustCompile(`(?i)^(https?://)?([a-z0-9-]+\.)*music\.apple\.com/([a-z]{2}/)?(album|playlist|song)/[a-zA-Z0-9\-._]+/(pl\.[a-zA-Z0-9]+|\d+)(\?.*)?$`),
	"spotify":     regexp.MustCompile(`(?i)^(https?://)?([a-z0-9-]+\.)*spotify\.com/(track|playlist|album|artist)/[a-zA-Z0-9]+(\?.*)?$`),
	"yt_playlist": regexp.MustCompile(`(?i)^(?:https?://)?(?:www\.)?(?:youtube\.com|music\.youtube\.com)/(?:playlist|watch)\?.*\blist=([\w-]+)`),
	"yt_music":    regexp.MustCompile(`(?i)^(?:https?://)?music\.youtube\.com/(?:watch|playlist)\?.*v=([\w-]+)`),
	"jiosaavn":    regexp.MustCompile(`(?i)^(https?://)?(www\.)?jiosaavn\.com/(song|featured)/[\w-]+/[a-zA-Z0-9_-]+$`),
	"soundcloud":  regexp.MustCompile(`(?i)^(https?://)?([a-z0-9-]+\.)*soundcloud\.com/[a-zA-Z0-9_-]+(/(sets)?/[a-zA-Z0-9_-]+)?(\?.*)?$`),
}

// NewApiData creates and initializes a new ApiData instance with the provided query.
func NewApiData(query string) *ApiData {
	return &ApiData{
		Query:    strings.TrimSpace(query),
		ApiUrl:   strings.TrimRight(config.Conf.ApiUrl, "/"),
		APIKey:   config.Conf.ApiKey,
		Patterns: apiPatterns,
	}
}

// IsValid checks if the query is a valid URL for any of the supported platforms.
// It returns true if the URL matches a known pattern, and false otherwise.
func (a *ApiData) IsValid() bool {
	if a.Query == "" {
		log.Printf("The query is empty.")
		return false
	}

	for _, pattern := range a.Patterns {
		if pattern.MatchString(a.Query) {
			return true
		}
	}
	return false
}

// GetInfo retrieves metadata for a track or playlist from the API.
// It returns a PlatformTracks object or an error if the request fails.
func (a *ApiData) GetInfo(ctx context.Context) (cache.PlatformTracks, error) {
	if !a.IsValid() {
		return cache.PlatformTracks{}, errors.New("the provided URL is invalid or the platform is not supported")
	}

	if a.ApiUrl == "" {
		return cache.PlatformTracks{}, errors.New("api url is not configured")
	}

	fullURL := fmt.Sprintf("%s/get_url?%s", a.ApiUrl, url.Values{"url": {a.Query}}.Encode())

	headers := map[string]string{}
	if a.APIKey != "" {
		headers["X-API-Key"] = a.APIKey
	}

	resp, err := sendRequest(ctx, http.MethodGet, fullURL, nil, headers)
	if err != nil {
		return cache.PlatformTracks{}, fmt.Errorf("the GetInfo request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return cache.PlatformTracks{}, fmt.Errorf("unexpected status code while fetching info: %s", resp.Status)
	}

	var data cache.PlatformTracks
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return cache.PlatformTracks{}, fmt.Errorf("failed to decode the GetInfo response: %w", err)
	}
	return data, nil
}

// Search queries the API for a track. The context can be used for timeouts or cancellations.
// If the query is a valid URL, it fetches the information directly.
// It returns a PlatformTracks object or an error if the search fails.
func (a *ApiData) Search(ctx context.Context) (cache.PlatformTracks, error) {
	if a.IsValid() {
		return a.GetInfo(ctx)
	}

	if a.ApiUrl == "" {
		return cache.PlatformTracks{}, errors.New("api url is not configured")
	}

	fullURL := fmt.Sprintf("%s/search?%s", a.ApiUrl, url.Values{
		"query": {a.Query},
		"limit": {"5"},
	}.Encode())

	headers := map[string]string{}
	if a.APIKey != "" {
		headers["X-API-Key"] = a.APIKey
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return cache.PlatformTracks{}, fmt.Errorf("failed to create the search request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return cache.PlatformTracks{}, fmt.Errorf("the search request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return cache.PlatformTracks{}, fmt.Errorf("unexpected status code during search: %s", resp.Status)
	}

	var data cache.PlatformTracks
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return cache.PlatformTracks{}, fmt.Errorf("failed to decode the search response: %w", err)
	}
	return data, nil
}

// GetTrack retrieves detailed information for a single track from the API.
// It returns a TrackInfo object or an error if the request fails.
func (a *ApiData) GetTrack(ctx context.Context) (cache.TrackInfo, error) {
	if a.ApiUrl == "" {
		return cache.TrackInfo{}, errors.New("api url is not configured")
	}

	fullURL := fmt.Sprintf("%s/track?%s", a.ApiUrl, url.Values{"url": {a.Query}}.Encode())

	headers := map[string]string{}
	if a.APIKey != "" {
		headers["X-API-Key"] = a.APIKey
	}

	resp, err := sendRequest(ctx, http.MethodGet, fullURL, nil, headers)
	if err != nil {
		return cache.TrackInfo{}, fmt.Errorf("the GetTrack request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return cache.TrackInfo{}, fmt.Errorf("unexpected status code while fetching the track: %s", resp.Status)
	}

	var data cache.TrackInfo
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return cache.TrackInfo{}, fmt.Errorf("failed to decode the GetTrack response: %w", err)
	}
	return data, nil
}

// downloadTrack downloads a track using the API. If the track is a YouTube video and video format is requested,
// it delegates the download to the YouTube downloader.
// It returns the file path of the downloaded track or an error if the download fails.
func (a *ApiData) downloadTrack(ctx context.Context, info cache.TrackInfo, video bool) (string, error) {
	// If youtube video + video requested -> fallback to youtube downloader
	if info.Platform == "youtube" && video {
		yt := NewYouTubeData(a.Query)
		return yt.downloadTrack(ctx, info, video)
	}

	// If API provides a /stream endpoint that returns direct media, try it first.
	if a.ApiUrl != "" {
		// Build stream URL: {apiUrl}/stream?url={original_query}[&type=audio&format=opus ...]
		streamBase := strings.TrimRight(a.ApiUrl, "/") + "/stream"
		values := url.Values{}
		values.Set("url", a.Query)
		// preserve any 'type' or 'format' parameters if already present in Query (user may provide full endpoint)
		streamURL := fmt.Sprintf("%s?%s", streamBase, values.Encode())

		headers := map[string]string{}
		if a.APIKey != "" {
			headers["X-API-Key"] = a.APIKey
		}

		resp, err := sendRequest(ctx, http.MethodGet, streamURL, nil, headers)
		if err == nil && resp != nil && resp.StatusCode == http.StatusOK {
			// Decide extension: for audio use .m4a always per requirement; for video use .mp4
			contentType := resp.Header.Get("Content-Type")
			ext := ".m4a"
			if strings.HasPrefix(contentType, "video/") {
				ext = ".mp4"
			} else {
				// try to sniff from content-disposition or mime type
				if cd := resp.Header.Get("Content-Disposition"); cd != "" {
					_, params, _ := mime.ParseMediaType(cd)
					if fn, ok := params["filename"]; ok {
						ext = filepath.Ext(fn)
						if ext == "" {
							ext = ".m4a"
						} else {
							// normalize video container if mp4 present
							if strings.EqualFold(ext, ".oga") || strings.EqualFold(ext, ".ogg") {
								ext = ".m4a"
							}
						}
					}
				}
			}

			// try to extract video id for naming
			videoID := ""
			if yt := NewYouTubeData(a.Query); yt != nil {
				videoID = yt.extractVideoID(a.Query)
			}
			if videoID == "" {
				// fallback to use a sanitized base name
				videoID = "stream_" + generateUniqueName("") // generateUniqueName returns with ext, pass empty ext then trim
				// strip any dot if present
				videoID = strings.TrimSuffix(videoID, ".")
			}

			// ensure downloads dir
			targetPath := filepath.Join(config.Conf.DownloadsDir, videoID+ext)
			if err := os.MkdirAll(filepath.Dir(targetPath), defaultDownloadDirPerm); err != nil {
				_ = resp.Body.Close()
				return "", fmt.Errorf("failed to create downloads directory: %w", err)
			}

			tmp := targetPath + ".part"
			f, ferr := os.Create(tmp)
			if ferr != nil {
				_ = resp.Body.Close()
				return "", fmt.Errorf("failed to create temp file: %w", ferr)
			}

			_, copyErr := io.Copy(f, resp.Body)
			_ = f.Close()
			_ = resp.Body.Close()
			if copyErr != nil {
				_ = os.Remove(tmp)
				return "", fmt.Errorf("failed to write stream to file: %w", copyErr)
			}

			if err := os.Rename(tmp, targetPath); err != nil {
				return "", fmt.Errorf("failed to rename temp file: %w", err)
			}

			return targetPath, nil
		}
		// if stream approach didn't work, continue to fallback
	}

	downloader, err := NewDownload(ctx, info)
	if err != nil {
		return "", fmt.Errorf("failed to initialize the download: %w", err)
	}

	filePath, err := downloader.Process()
	if err != nil {
		if info.Platform == "youtube" {
			yt := NewYouTubeData(a.Query)
			return yt.downloadTrack(ctx, info, video)
		}
		return "", fmt.Errorf("the download process failed: %w", err)
	}
	return filePath, nil
}
