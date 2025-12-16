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
	"regexp"
	"strings"

	"ashokshau/tgmusic/src/core/cache"
)

type ytSearchResp struct {
	Contents struct {
		TwoColumnSearchResultsRenderer struct {
			PrimaryContents struct {
				SectionListRenderer struct {
					Contents []struct {
						ItemSectionRenderer struct {
							Contents []struct {
								VideoRenderer struct {
									VideoID string `json:"videoId"`
									Title   struct {
										Runs []struct {
											Text string `json:"text"`
										} `json:"runs"`
									} `json:"title"`
									Thumbnail struct {
										Thumbnails []struct {
											URL string `json:"url"`
										} `json:"thumbnails"`
									} `json:"thumbnail"`
									LengthText struct {
										SimpleText string `json:"simpleText"`
									} `json:"lengthText"`
									ShortViewCountText struct {
										SimpleText string `json:"simpleText"`
									} `json:"shortViewCountText"`
									OwnerText struct {
										Runs []struct {
											Text string `json:"text"`
										} `json:"runs"`
									} `json:"ownerText"`
								} `json:"videoRenderer"`
							} `json:"contents"`
						} `json:"itemSectionRenderer"`
					} `json:"contents"`
				} `json:"sectionListRenderer"`
			} `json:"primaryContents"`
		} `json:"twoColumnSearchResultsRenderer"`
	} `json:"contents"`
}

var ytURL = regexp.MustCompile(`(?:v=|youtu\.be/|shorts/)([\w-]{11})`)

func searchYouTube(query string) ([]cache.MusicTrack, error) {
	if id := extractVideoID(query); id != "" {
		return []cache.MusicTrack{
			{
				URL:      "https://www.youtube.com/watch?v=" + id,
				ID:       id,
				Name:     "Unknown",
				Cover:    "",
				Duration: 0,
				Views:    "",
				Channel:  "",
				Platform: "youtube",
			},
		}, nil
	}

	payload := map[string]interface{}{
		"context": map[string]interface{}{
			"client": map[string]interface{}{
				"clientName":    "WEB",
				"clientVersion": "2.20241210.01.00",
				"hl":            "en",
				"gl":            "US",
			},
		},
		"query": query,
	}

	b, _ := json.Marshal(payload)
	req, err := http.NewRequest(
		"POST",
		"https://www.youtube.com/youtubei/v1/search?prettyPrint=false",
		bytes.NewReader(b),
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", "https://www.youtube.com")
	req.Header.Set("Referer", "https://www.youtube.com/")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data ytSearchResp
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	var tracks []cache.MusicTrack

	for _, c := range data.Contents.TwoColumnSearchResultsRenderer.PrimaryContents.
		SectionListRenderer.Contents {
		for _, it := range c.ItemSectionRenderer.Contents {
			v := it.VideoRenderer
			if v.VideoID == "" {
				continue
			}
			title := ""
			if len(v.Title.Runs) > 0 {
				title = v.Title.Runs[0].Text
			}
			thumb := ""
			if len(v.Thumbnail.Thumbnails) > 0 {
				thumb = v.Thumbnail.Thumbnails[0].URL
			}
			channel := ""
			if len(v.OwnerText.Runs) > 0 {
				channel = v.OwnerText.Runs[0].Text
			}
			duration := parseDuration(v.LengthText.SimpleText)

			tracks = append(tracks, cache.MusicTrack{
				URL:      "https://www.youtube.com/watch?v=" + v.VideoID,
				ID:       v.VideoID,
				Name:     title,
				Cover:    thumb,
				Duration: duration,
				Views:    v.ShortViewCountText.SimpleText,
				Channel:  channel,
				Platform: "youtube",
			})
		}
	}

	return tracks, nil
}

func extractVideoID(s string) string {
	m := ytURL.FindStringSubmatch(s)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

func parseDuration(s string) int {
	if s == "" {
		return 0
	}
	parts := strings.Split(s, ":")
	total := 0
	m := 1
	for i := len(parts) - 1; i >= 0; i-- {
		total += atoi(parts[i]) * m
		m *= 60
	}
	return total
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			n = n*10 + int(r-'0')
		}
	}
	return n
}
