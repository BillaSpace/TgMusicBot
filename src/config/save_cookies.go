/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package config

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const cookiesDr = "src/cookies"

func fetchContent(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		ct := strings.ToLower(resp.Header.Get("content-type"))
		if strings.Contains(ct, "text") {
			return string(body), nil
		}
	}

	parts := strings.Split(strings.Trim(url, "/"), "/")
	id := parts[len(parts)-1]

	var rawURL string
	if strings.Contains(url, "pastebin.com") {
		rawURL = fmt.Sprintf("https://pastebin.com/raw/%s", id)
	} else if strings.Contains(url, "batbin.me") {
		rawURL = fmt.Sprintf("https://batbin.me/raw/%s", id)
	} else {
		rawURL = url
	}

	resp, err = http.Get(rawURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

func saveContent(url, content string) (string, error) {
	parts := strings.Split(strings.Trim(url, "/"), "/")
	filename := parts[len(parts)-1]
	if filename == "" {
		filename = "cookies"
	}
	if !strings.HasSuffix(filename, ".txt") {
		filename += ".txt"
	}

	path := filepath.Join(cookiesDr, filename)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	_, err = f.WriteString(content)
	if err != nil {
		return "", err
	}

	return path, nil
}

func isExpired(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	return time.Since(info.ModTime()) > 72*time.Hour
}

func saveAllCookies(urls []string) {
	for _, url := range urls {
		parts := strings.Split(strings.Trim(url, "/"), "/")
		name := parts[len(parts)-1]
		if !strings.HasSuffix(name, ".txt") {
			name += ".txt"
		}
		path := filepath.Join(cookiesDr, name)

		if !isExpired(path) {
			Conf.CookiesPath = append(Conf.CookiesPath, path)
			continue
		}

		_ = os.Remove(path)

		content, err := fetchContent(url)
		if err != nil {
			fmt.Println("Error fetching:", err)
			continue
		}

		path, err = saveContent(url, content)
		if err != nil {
			fmt.Println("Error saving:", err)
			continue
		}

		Conf.CookiesPath = append(Conf.CookiesPath, path)
	}
}
