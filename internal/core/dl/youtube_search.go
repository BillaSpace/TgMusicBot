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
	"errors"
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

	// Extract ytInitialData robustly via brace-matching fallback 
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
		// Some responses wrap differently, try alternative path having randomised tokens
		contents = dig(data, "contents", "twoColumnSearchResultsRenderer")
	}

	if contents == nil {
		return nil, fmt.Errorf("no contents")
	}

	var tracks []cache.MusicTrack
	parseSearchResults(contents, &tracks)

	return tracks, nil
}

// extractInitialData tries multiple quick regexes first then falls back to a robust brace-matching extractor.
func extractInitialData(body []byte) ([]byte, error) {
	// Quick regex attempts (covers many cases)
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?s)var\s+ytInitialData\s*=\s*(\{.*?\})\s*;\s*</script>`),
		regexp.MustCompile(`(?s)"ytInitialData"\s*:\s*(\{.*?\})\s*,\s*"(?:ytcfg|responseContext)"`),
		regexp.MustCompile(`(?s)window\[\s*["']ytInitialData["']\s*\]\s*=\s*(\{.*?\})\s*;\s*</script>`),
		regexp.MustCompile(`(?s)var\s+ytInitialData\s*=\s*(\{.*?\})\s*;\s*`),
	}

	for _, re := range patterns {
		if m := re.FindSubmatch(body); len(m) >= 2 && len(m[1]) > 0 {
			return m[1], nil
		}
	}

	// Fallback: find start of ytInitialData and match braces to extract full JSON.
	// Handle forms like: var ytInitialData = { ... } ;  OR  "ytInitialData": { ... },
	bodyStr := string(body)
	keys := []string{"ytInitialData"}
	for _, key := range keys {
		// look for patterns like: ytInitialData = {  OR  "ytInitialData": {
		idx := strings.Index(bodyStr, key)
		if idx == -1 {
			continue
		}
		// find the first '{' after the key
		braceIdx := strings.Index(bodyStr[idx:], "{")
		if braceIdx == -1 {
			continue
		}
		start := idx + braceIdx
		// Now perform brace matching
		end, ok := findMatchingBrace(bodyStr, start)
		if !ok {
			continue
		}
		jsonText := bodyStr[start : end+1]
		// quick sanity check: must start with '{' and be valid JSON
		var tmp interface{}
		if err := json.Unmarshal([]byte(jsonText), &tmp); err == nil {
			return []byte(jsonText), nil
		}
	}
	return nil, errors.New("ytInitialData not found")
}

// findMatchingBrace returns the index of the matching '}' for the '{' at startIdx.
func findMatchingBrace(s string, startIdx int) (int, bool) {
	depth := 0
	inStr := false
	esc := false
	for i := startIdx; i < len(s); i++ {
		c := s[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			continue
		}
		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return -1, false
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
		if vidRaw := dig(v, "videoRenderer"); vidRaw != nil {
			if vid, ok := vidRaw.(map[string]interface{}); ok {
				id := safeString(vid["videoId"])
				if id != "" {
					title := safeString(dig(vid, "title", "runs", 0, "text"))
					if title == "" {
						// fallback: simpleText
						title = safeString(dig(vid, "title", "simpleText"))
					}

					// thumbnails: try last, then first, handle protocol-relative URLs
					var thumb string
					if th := dig(vid, "thumbnail", "thumbnails"); th != nil {
						if arr, ok := th.([]interface{}); ok && len(arr) > 0 {
							// prefer last entry
							last := arr[len(arr)-1]
							thumb = safeString(last)
							if thumb == "" {
								thumb = safeString(arr[0])
							}
						}
					}
					// additional fallback
					if thumb == "" {
						thumb = safeString(dig(vid, "thumbnail", "thumbnails", 0, "url"))
					}
					if strings.HasPrefix(thumb, "//") {
						thumb = "https:" + thumb
					}
					if !strings.HasPrefix(thumb, "http") {
						thumb = ""
					}

					durationText := safeString(dig(vid, "lengthText", "simpleText"))
					duration := parseDuration(durationText)

					// Filter out unavailable / private / deleted videos by checking accessibility or badges
					if safeString(dig(vid, "isPlayable")) == "false" || safeString(dig(vid, "isPlayable")) == "0" {
						// skip
					} else {
						*tracks = append(*tracks, cache.MusicTrack{
							URL:      "https://www.youtube.com/watch?v=" + id,
							Name:     title,
							ID:       id,
							Cover:    thumb,
							Duration: duration,
							Platform: "youtube",
						})
					}
				}
			}
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
		default:
			return nil
		}
	}
	return curr
}

// safely cast to string
func safeString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	// If it's a map (thumbnail object), try url field or text fields
	if m, ok := v.(map[string]interface{}); ok {
		if u, ok := m["url"].(string); ok {
			return u
		}
		if t, ok := m["text"].(string); ok {
			return t
		}
	}
	// If it's a float64 (JSON numbers), return its integer form
	if f, ok := v.(float64); ok {
		return fmt.Sprintf("%.0f", f)
	}
	return ""
}

// parse duration like "3:45" -> 225 seconds (also handles "1:02:03")
func parseDuration(s string) int {
	if s == "" {
		return 0
	}
	// Remove any non-digit and non-colon characters (e.g., "Duration: 3:45" or "3:45 avg. rating")
	re := regexp.MustCompile(`[\d:]+`)
	m := re.FindString(s)
	if m == "" {
		return 0
	}
	parts := strings.Split(m, ":")
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
