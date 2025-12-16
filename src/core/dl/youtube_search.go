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
	"strings"

	"ashokshau/tgmusic/src/core/cache"
)

type ytTextRun struct {
	Text string `json:"text"`
}

type ytThumb struct {
	URL string `json:"url"`
}

type ytSearchResp struct {
	Contents struct {
		TwoColumnSearchResultsRenderer struct {
			PrimaryContents struct {
				SectionListRenderer struct {
					Contents []struct {
						ItemSectionRenderer struct {
							Contents []struct {
								VideoRenderer    *ytVideoRenderer    `json:"videoRenderer"`
								PlaylistRenderer *ytPlaylistRenderer `json:"playlistRenderer"`
							} `json:"contents"`
						} `json:"itemSectionRenderer"`
					} `json:"contents"`
				} `json:"sectionListRenderer"`
			} `json:"primaryContents"`
		} `json:"twoColumnSearchResultsRenderer"`
	} `json:"contents"`
}

type ytVideoRenderer struct {
	VideoID string `json:"videoId"`

	Title struct {
		Runs []ytTextRun `json:"runs"`
	} `json:"title"`

	Thumbnail struct {
		Thumbnails []ytThumb `json:"thumbnails"`
	} `json:"thumbnail"`

	LengthText struct {
		SimpleText string `json:"simpleText"`
	} `json:"lengthText"`

	ShortViewCountText struct {
		SimpleText string `json:"simpleText"`
	} `json:"shortViewCountText"`

	OwnerText struct {
		Runs []ytTextRun `json:"runs"`
	} `json:"ownerText"`
}

type ytPlaylistRenderer struct {
	PlaylistID string `json:"playlistId"`

	Title struct {
		Runs []ytTextRun `json:"runs"`
	} `json:"title"`

	Thumbnail struct {
		Thumbnails []ytThumb `json:"thumbnails"`
	} `json:"thumbnail"`

	ShortBylineText struct {
		Runs []ytTextRun `json:"runs"`
	} `json:"shortBylineText"`

	VideoCount string `json:"videoCount"`
}

func searchYouTube(query string) ([]cache.MusicTrack, error) {
	payload := map[string]any{
		"context": map[string]any{
			"client": map[string]any{
				"clientName":    "WEB",
				"clientVersion": "2.20241210.01.00",
				"hl":            "en",
				"gl":            "US",
			},
		},
		"query": query,
	}

	body, _ := json.Marshal(payload)

	req, err := http.NewRequest(
		"POST",
		"https://www.youtube.com/youtubei/v1/search?prettyPrint=false",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64)")
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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data ytSearchResp
	if err := json.Unmarshal(respBody, &data); err != nil {
		return nil, err
	}

	var tracks []cache.MusicTrack

	sections :=
		data.Contents.
			TwoColumnSearchResultsRenderer.
			PrimaryContents.
			SectionListRenderer.
			Contents

	for _, section := range sections {
		for _, item := range section.ItemSectionRenderer.Contents {

			if v := item.VideoRenderer; v != nil && v.VideoID != "" {
				tracks = append(tracks, cache.MusicTrack{
					ID:       v.VideoID,
					URL:      "https://www.youtube.com/watch?v=" + v.VideoID,
					Name:     textRun(v.Title.Runs),
					Cover:    thumb(v.Thumbnail.Thumbnails),
					Duration: parseDuration(v.LengthText.SimpleText),
					Views:    v.ShortViewCountText.SimpleText,
					Channel:  textRun(v.OwnerText.Runs),
					Platform: "youtube",
				})
				continue
			}

			if p := item.PlaylistRenderer; p != nil && p.PlaylistID != "" {
				tracks = append(tracks, cache.MusicTrack{
					ID:       p.PlaylistID,
					URL:      "https://www.youtube.com/playlist?list=" + p.PlaylistID,
					Name:     textRun(p.Title.Runs),
					Cover:    thumb(p.Thumbnail.Thumbnails),
					Duration: 0,
					Views:    p.VideoCount + " videos",
					Channel:  textRun(p.ShortBylineText.Runs),
					Platform: "youtube",
				})
			}
		}
	}

	return tracks, nil
}

func parseDuration(s string) int {
	if s == "" {
		return 0
	}

	parts := strings.Split(s, ":")
	total := 0
	multiplier := 1

	for i := len(parts) - 1; i >= 0; i-- {
		total += atoi(parts[i]) * multiplier
		multiplier *= 60
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

func textRun(r []ytTextRun) string {
	if len(r) > 0 {
		return r[0].Text
	}
	return ""
}

func thumb(t []ytThumb) string {
	if len(t) > 0 {
		return t[0].URL
	}
	return ""
}
