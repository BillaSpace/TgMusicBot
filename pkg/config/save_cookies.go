package config

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var tmpDir = "src/cookies"

// fetchContent downloads content from Pastebin, Batbin, or direct .txt URLs.
// It returns the content of the URL as a string and an error if any.
func fetchContent(url string) (string, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return "", fmt.Errorf("empty URL provided")
	}

	var rawURL string
	if strings.Contains(url, "pastebin.com") {
		// Pastebin
		parts := strings.Split(strings.Trim(url, "/"), "/")
		id := parts[len(parts)-1]
		rawURL = fmt.Sprintf("https://pastebin.com/raw/%s", id)
	} else if strings.Contains(url, "batbin.me") {
		// Batbin
		parts := strings.Split(strings.Trim(url, "/"), "/")
		id := parts[len(parts)-1]
		rawURL = fmt.Sprintf("https://batbin.me/raw/%s", id)
	} else {
		// Direct URL (e.g. https://.../cookies.txt)
		rawURL = url
	}

	resp, err := http.Get(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to GET %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d for %s", resp.StatusCode, rawURL)
	}

	// Optional: Verify it's a text response
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text") && !strings.Contains(contentType, "json") {
		return "", fmt.Errorf("unsupported content type %s from %s", contentType, rawURL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read body from %s: %w", rawURL, err)
	}

	return string(body), nil
}

// saveContent saves content to a file in src/cookies and returns the file path.
func saveContent(url, content string) (string, error) {
	parts := strings.Split(strings.Trim(url, "/"), "/")
	filename := parts[len(parts)-1]

	if filename == "" || !strings.HasSuffix(filename, ".txt") {
		filename = strings.ReplaceAll(filename, ".", "_")
		if filename == "" {
			filename = "file_" + strings.ReplaceAll(strings.ReplaceAll(url, "/", "_"), ":", "_")
		}
		filename += ".txt"
	}

	filePath := filepath.Join(tmpDir, filename)

	// Ensure directory exists
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create dir %s: %w", tmpDir, err)
	}

	f, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to create file %s: %w", filePath, err)
	}
	defer f.Close()

	if _, err := f.WriteString(content); err != nil {
		return "", fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	return filePath, nil
}

// saveAllCookies downloads all URLs and stores paths in Conf.CookiesPath.
func saveAllCookies(urls []string) {
	for _, url := range urls {
		content, err := fetchContent(url)
		if err != nil {
			fmt.Println("Error fetching:", err)
			continue
		}

		path, err := saveContent(url, content)
		if err != nil {
			fmt.Println("Error saving:", err)
			continue
		}

		Conf.CookiesPath = append(Conf.CookiesPath, path)
	}
}