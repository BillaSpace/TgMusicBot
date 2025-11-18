/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package dl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"ashokshau/tgmusic/src/core/cache"
)

// searchYouTube scrapes YouTube results page or returns direct track for YouTube URLs
func searchYouTube(query string) ([]cache.MusicTrack, error) {
	// If query is a youtube URL or contains youtu.be, try to extract video id and return a direct result
	if id := extractVideoIDFromAny(query); id != "" {
		track := cache.MusicTrack{
			URL:      "https://www.youtube.com/watch?v=" + id,
			ID:       id,
			Platform: "youtube",
		}

		// Try oEmbed to get title and thumbnail
		oembedURL := "https://www.youtube.com/oembed?url=" + url.QueryEscape(track.URL) + "&format=json"
		req, _ := http.NewRequest("GET", oembedURL, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64)")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var oe struct {
					Title  string `json:"title"`
					Author string `json:"author_name"`
					Thumb  string `json:"thumbnail_url"`
				}
				if b, err := io.ReadAll(resp.Body); err == nil {
					if json.Unmarshal(b, &oe) == nil {
						track.Name = oe.Title
						if oe.Thumb != "" {
							track.Cover = oe.Thumb
						}
					}
				}
			}
		}
		// If oEmbed failed, we still return the basic track (bot can fetch metadata later)
		return []cache.MusicTrack{track}, nil
	}

	encoded := url.QueryEscape(query)
	searchURL := "https://www.youtube.com/results?search_query=" + encoded

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64)")
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

	// Try robust JSON extraction (handles different JS assignments)
	jsJSON, err := extractJSONFromJS(body, "ytInitialData")
	if err != nil {
		// fallback to older regex (if needed)
		re := regexp.MustCompile(`var ytInitialData = (.*?);\s*</script>`)
		match := re.FindSubmatch(body)
		if len(match) < 2 {
			return nil, fmt.Errorf("ytInitialData not found: %w", err)
		}
		jsJSON = match[1]
	}

	var data map[string]interface{}
	if err := json.Unmarshal(jsJSON, &data); err != nil {
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

// extractJSONFromJS finds the JS variable assignment (like ytInitialData) and returns the JSON bytes
func extractJSONFromJS(body []byte, varName string) ([]byte, error) {
	// Search for occurrences like: var ytInitialData = { ... };
	// or window["ytInitialData"] = { ... };
	idx := bytes.Index(body, []byte(varName))
	if idx == -1 {
		return nil, fmt.Errorf("%s not found", varName)
	}

	// Find the first '{' after the varName
	start := bytes.IndexByte(body[idx:], '{')
	if start == -1 {
		return nil, fmt.Errorf("opening brace not found after %s", varName)
	}
	start += idx

	// Balance braces to find the end
	depth := 0
	for i := start; i < len(body); i++ {
		switch body[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				// return the JSON slice (inclusive)
				return body[start : i+1], nil
			}
		}
	}
	return nil, fmt.Errorf("could not balance braces for %s", varName)
}

// extractVideoIDFromAny extracts video id from many YouTube URL patterns
func extractVideoIDFromAny(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// If it's just an ID (11 chars) heuristics: letters, digits, - _
	// but better to require typical URL patterns first
	u := s
	// If missing scheme, add one for parsing
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		// treat plain youtube id? maybe not; just try to parse known prefixes
		if strings.HasPrefix(u, "youtu.be/") || strings.Contains(u, "youtube.com") || strings.Contains(u, "youtu.be") {
			u = "https://" + u
		}
	}
	parsed, err := url.Parse(u)
	if err == nil && parsed.Host != "" {
		host := parsed.Hostname()
		if host == "youtu.be" {
			// path is /{id}
			id := strings.TrimPrefix(parsed.Path, "/")
			// strip params like ; or extra
			if idx := strings.IndexAny(id, "&?;"); idx != -1 {
				id = id[:idx]
			}
			return id
		}
		if strings.Contains(host, "youtube.com") {
			// check query v=...
			if q := parsed.Query().Get("v"); q != "" {
				return q
			}
			// sometimes path like /shorts/{id} or /watch/{id}
			parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
			if len(parts) > 1 {
				// shorts/{id}
				if parts[0] == "shorts" || parts[0] == "embed" {
					return parts[1]
				}
			}
		}
	}

	// As a last resort, try regexp for typical IDs in the string
	re := regexp.MustCompile(`(?i)(?:v=|v/|youtu\.be/|embed/|shorts/)([A-Za-z0-9_-]{8,})`)
	if m := re.FindStringSubmatch(s); len(m) > 1 {
		return m[1]
	}

	// nothing found
	return ""
}

// Recursively find items
func parseSearchResults(node interface{}, tracks *[]cache.MusicTrack) {
	switch v := node.(type) {
	case []interface{}:
		for _, item := range v {
			parseSearchResults(item, tracks)
		}
	case map[string]interface{}:
		if vidRaw := dig(v, "videoRenderer"); vidRaw != nil {
			if vid, ok := vidRaw.(map[string]interface{}); ok {
				id := safeString(vid["videoId"])
				title := safeString(dig(vid, "title", "runs", 0, "text"))
				// Try to pick the largest thumbnail available
				thumb := ""
				if tArr := dig(vid, "thumbnail", "thumbnails"); tArr != nil {
					if ta, ok := tArr.([]interface{}); ok && len(ta) > 0 {
						// pick last thumbnail (usually highest res)
						if last := ta[len(ta)-1]; last != nil {
							thumb = safeString(dig(last, "url"))
						}
					}
				}
				if thumb == "" {
					thumb = safeString(dig(vid, "thumbnail", "thumbnails", 0, "url"))
				}
				durationText := safeString(dig(vid, "lengthText", "simpleText"))
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
		}
		// otherwise search children
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
			if arr, ok := curr.([]interface{}); ok && len(arr) > key {
				curr = arr[key]
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
	return ""
}

// parse duration like "3:45" -> 225 seconds
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
