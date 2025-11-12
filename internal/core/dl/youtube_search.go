/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package dl

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/AshokShau/TgMusicBot/internal/core/cache"
)

// searchYouTube scrapes YouTube results page
func searchYouTube(query string) ([]cache.MusicTrack, error) {
	encoded := url.QueryEscape(query)
	searchURL := "https://www.youtube.com/results?search_query=" + encoded

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64)")
	// Helps keep the markup predictable so regexes match reliably.
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Robustly extract ytInitialData JSON (YouTube often inserts newlines).
	jsonBlob, err := extractInitialData(body)
	if err != nil {
		return nil, err
	}

	var data map[string]interface{}
	if err := json.Unmarshal(jsonBlob, &data); err != nil {
		return nil, err
	}

	contents := dig(data, "contents", "twoColumnSearchResultsRenderer",
		"primaryContents", "sectionListRenderer", "contents")

	if contents == nil {
		return nil, fmt.Errorf("no contents")
	}

	var tracks []cache.MusicTrack
	parseSearchResults(contents, &tracks)

	return tracks, nil
}

// Try multiple regex patterns to find ytInitialData JSON.
// The (?s) flag makes '.' match newlines so we don't choke on multi-line scripts.
func extractInitialData(body []byte) ([]byte, error) {
	patterns := []*regexp.Regexp{
		// Classic form: var ytInitialData = { ... };
		regexp.MustCompile(`(?s)var\s+ytInitialData\s*=\s*(\{.*?\});\s*</script>`),
		// Inlined object form: "ytInitialData": { ... },
		regexp.MustCompile(`(?s)"ytInitialData"\s*:\s*(\{.*?\})\s*,\s*"(?:ytcfg|responseContext)"`),
		// Fallback: window["ytInitialData"] = { ... };
		regexp.MustCompile(`(?s)window\[\s*["']ytInitialData["']\s*\]\s*=\s*(\{.*?\});\s*</script>`),
	}

	for _, re := range patterns {
		if m := re.FindSubmatch(body); len(m) >= 2 && len(m[1]) > 0 {
			return m[1], nil
		}
	}
	return nil, fmt.Errorf("ytInitialData not found")
}

// Recursively find items
func parseSearchResults(node interface{}, tracks *[]cache.MusicTrack) {
	switch v := node.(type) {
	case []interface{}:
		for _, item := range v {
			parseSearchResults(item, tracks)
		}
	case map[string]interface{}:
		// Direct video result
		if vid, ok := dig(v, "videoRenderer").(map[string]interface{}); ok {
			id := safeString(vid["videoId"])
			if id == "" {
				return
			}
			title := safeString(dig(vid, "title", "runs", 0, "text"))

			// pick the last (usually largest) thumbnail if available
			thumb := safeString(dig(vid, "thumbnail", "thumbnails", -1))
			if thumb == "" {
				thumb = safeString(dig(vid, "thumbnail", "thumbnails", 0, "url"))
			}
			// If -1 returns the entire object, pull url safely
			if !strings.HasPrefix(thumb, "http") {
				thumb = safeString(dig(vid, "thumbnail", "thumbnails", 0, "url"))
			}

			durationText := safeString(dig(vid, "lengthText", "simpleText"))
			// Some results (Shorts/Live) may not have lengthText
			duration := parseDuration(durationText)

			*tracks = append(*tracks, cache.MusicTrack{
				URL:      "https://www.youtube.com/watch?v=" + id,
				Name:     title,
				ID:       id,
				Cover:    thumb,
				Duration: duration,
				Platform: "youtube",
			})
			return
		}

		// Keep walking nested structures
		for _, child := range v {
			parseSearchResults(child, tracks)
		}
	}
}

// safely dig into nested JSON
func dig(m interface{}, path ...interface{}) interface{} {
	curr := m
	for _, p := range path {
		switch key := p.(type) {
		case string:
			if mm, ok := curr.(map[string]interface{}); ok {
				curr = mm[key]
			} else {
				return nil
			}
		case int:
			if arr, ok := curr.([]interface{}); ok {
				idx := key
				// support negative indices (e.g., -1 = last)
				if idx < 0 {
					idx = len(arr) + idx
				}
				if idx >= 0 && idx < len(arr) {
					curr = arr[idx]
				} else {
					return nil
				}
			} else {
				return nil
			}
		}
	}
	return curr
}

// safely cast to string
func safeString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	// If it's a map (thumbnail object), try url field
	if m, ok := v.(map[string]interface{}); ok {
		if u, ok := m["url"].(string); ok {
			return u
		}
	}
	return ""
}

// parse duration like "3:45" -> 225 seconds (also handles "1:02:03")
func parseDuration(s string) int {
	if s == "" {
		return 0
	}
	parts := strings.Split(s, ":")
	total := 0
	multiplier := 1

	// Process from right to left (seconds → minutes → hours)
	for i := len(parts) - 1; i >= 0; i-- {
		total += atoi(parts[i]) * multiplier
		multiplier *= 60
	}
	return total
}

// atoi converts a string to an integer
func atoi(s string) int {
	var n int
	for _, r := range s {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
		}
	}
	return n
}
