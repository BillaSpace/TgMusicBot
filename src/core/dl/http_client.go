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
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ashokshau/tgmusic/src/config"
)

const (
	defaultRequestTimeout = 30 * time.Second
	defaultConnectTimeout = 10 * time.Second
	maxRetries            = 2
	initialBackoff        = 1 * time.Second
)

var client = &http.Client{
	Timeout: defaultRequestTimeout,
	Transport: &http.Transport{
		TLSHandshakeTimeout:   defaultConnectTimeout,
		ResponseHeaderTimeout: defaultRequestTimeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
	},
}

// sendRequest performs an HTTP request with a given context, method, URL, body, and headers.
// It includes retry logic with exponential backoff for temporary network errors and server-side issues.
// It returns an HTTP response or an error if the request fails after all retries.
func sendRequest(ctx context.Context, method, fullURL string, body io.Reader, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept", "*/*")

	// Only set headers if provided (API key optional)
	if headers != nil {
		for k, v := range headers {
			if v != "" {
				req.Header.Set(k, v)
			}
		}
	}

	var resp *http.Response
	var reqErr error
	backoff := initialBackoff

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(backoff)
			backoff *= 2
		}

		resp, reqErr = client.Do(req)
		if reqErr == nil {
			if resp.StatusCode < 500 {
				return resp, nil // Success
			}
			if err := resp.Body.Close(); err != nil {
				log.Printf("failed to close response body: %v", err)
			}
			reqErr = fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		} else if isTemporaryError(reqErr) {
			log.Printf("Temporary error on attempt %d/%d: %v", attempt+1, maxRetries, reqErr)
			continue // Retry on temporary errors
		} else {
			break // Do not retry on permanent errors
		}
	}

	if reqErr == nil {
		reqErr = fmt.Errorf("request failed after %d attempts", maxRetries)
	}

	return nil, fmt.Errorf("request failed: %w", reqErr)
}

// isTemporaryError determines if an error is temporary and thus worth retrying.
// It returns true for network timeouts and temporary operational errors.
func isTemporaryError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}
	return false
}

// generateUniqueName creates a pseudo-random filename using a combination of the current timestamp and a random number.
// It takes a file extension and returns a unique filename.
func generateUniqueName(ext string) string {
	n, _ := rand.Int(rand.Reader, big.NewInt(99999))
	return fmt.Sprintf("%d_%05d%s", time.Now().UnixNano(), n.Int64(), ext)
}

// determineFilename safely determines a valid filename for a download.
// It prioritizes the Content-Disposition header, falls back to the URL path, and generates a unique name if neither is available.
// It returns a secure and sanitized filename.
func determineFilename(urlStr, contentDisp string) string {
	if filename := extractFilename(contentDisp); filename != "" {
		return filepath.Join(config.Conf.DownloadsDir, sanitizeFilename(filename))
	}

	if parsedURL, err := url.Parse(urlStr); err == nil {
		filename := path.Base(parsedURL.Path)
		if filename != "" && filename != "/" && !strings.Contains(filename, "?") {
			return filepath.Join(config.Conf.DownloadsDir, sanitizeFilename(filename))
		}
	}

	return filepath.Join(config.Conf.DownloadsDir, generateUniqueName(".tmp"))
}

// writeToFile writes data from an io.Reader to a specified file.
// It returns an error if file creation or writing fails.
func writeToFile(filename string, data io.Reader) error {
	out, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create the file: %w", err)
	}
	defer func(out *os.File) {
		_ = out.Close()
	}(out)

	if _, err := io.Copy(out, data); err != nil {
		return fmt.Errorf("failed to write to the file: %w", err)
	}

	return nil
}

const (
	downloadTimeout         = 2 * time.Minute
	defaultDownloadDirPerm  = 0o755
	chunkSize               = 8 * 1024 * 1024 // 8 MiB
)

// DownloadFile downloads a file from a URL and saves it to a local path.
// It supports overwriting existing files and determines the filename automatically if not provided.
// This implementation attempts chunked downloads using HTTP Range requests (8 MiB chunks).
// It falls back to a single-request download when the server does not support ranges.
func DownloadFile(ctx context.Context, urlStr, fileName string, overwrite bool) (string, error) {
	if urlStr == "" {
		return "", errors.New("an empty URL was provided")
	}

	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	// First, do a HEAD request to check for Accept-Ranges and Content-Length
	headReq, err := http.NewRequestWithContext(ctx, http.MethodHead, urlStr, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create HEAD request: %w", err)
	}
	headReq.Header.Set("User-Agent", "Mozilla/5.0 (compatible)")

	headResp, err := client.Do(headReq)
	if err != nil {
		// If HEAD fails, proceed with a GET fallback (server may not support HEAD)
	} else {
		_ = headResp.Body.Close()
	}

	acceptRanges := false
	var totalSize int64 = -1
	if headResp != nil {
		if ar := headResp.Header.Get("Accept-Ranges"); strings.Contains(strings.ToLower(ar), "bytes") {
			acceptRanges = true
		}
		if cl := headResp.Header.Get("Content-Length"); cl != "" {
			if v, perr := strconv.ParseInt(cl, 10, 64); perr == nil {
				totalSize = v
			}
		}
	}

	// If we couldn't determine content-length from HEAD, try a lightweight GET with Range 0-0 to obtain headers
	if totalSize < 0 {
		getReq, gerr := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
		if gerr == nil {
			getReq.Header.Set("Range", "bytes=0-0")
			getReq.Header.Set("User-Agent", "Mozilla/5.0 (compatible)")
			getResp, derr := client.Do(getReq)
			if derr == nil {
				defer func() { _ = getResp.Body.Close() }()
				if getResp.StatusCode == http.StatusPartialContent {
					acceptRanges = true
					if cl := getResp.Header.Get("Content-Range"); cl != "" {
						// Content-Range: bytes 0-0/12345
						if parts := strings.Split(cl, "/"); len(parts) == 2 {
							if v, perr := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64); perr == nil {
								totalSize = v
							}
						}
					}
				} else if getResp.StatusCode == http.StatusOK {
					// server returned full content for 0-0, try to read Content-Length
					if cl := getResp.Header.Get("Content-Length"); cl != "" {
						if v, perr := strconv.ParseInt(cl, 10, 64); perr == nil {
							totalSize = v
						}
					}
				}
			}
		}
	}

	// Determine filename if not provided (use final headers from headResp if available)
	if fileName == "" {
		var cd string
		if headResp != nil {
			cd = headResp.Header.Get("Content-Disposition")
		}
		fileName = determineFilename(urlStr, cd)
	}

	// If file exists and overwrite is false, return early
	if !overwrite {
		if _, err := os.Stat(fileName); err == nil {
			return fileName, nil
		}
	}

	// Ensure download directory exists
	if err := os.MkdirAll(filepath.Dir(fileName), defaultDownloadDirPerm); err != nil {
		return "", fmt.Errorf("failed to create the directory: %w", err)
	}

	tempPath := fileName + ".part"

	// If server supports ranges and we know total size -> perform chunked download
	if acceptRanges && totalSize > 0 {
		f, ferr := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY, 0o644)
		if ferr != nil {
			return "", fmt.Errorf("failed to open temp file for writing: %w", ferr)
		}
		// Preallocate (best-effort)
		_ = f.Truncate(totalSize)
		_ = f.Close()

		out, oerr := os.OpenFile(tempPath, os.O_WRONLY, 0o644)
		if oerr != nil {
			return "", fmt.Errorf("failed to open temp file for chunked writing: %w", oerr)
		}
		defer func() { _ = out.Close() }()

		var offset int64 = 0
		for offset < totalSize {
			end := offset + chunkSize - 1
			if end >= totalSize {
				end = totalSize - 1
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
			if err != nil {
				return "", fmt.Errorf("failed to create ranged request: %w", err)
			}
			rangeHeader := fmt.Sprintf("bytes=%d-%d", offset, end)
			req.Header.Set("Range", rangeHeader)
			req.Header.Set("User-Agent", "Mozilla/5.0 (compatible)")

			resp, err := client.Do(req)
			if err != nil {
				return "", fmt.Errorf("ranged request failed for %s: %w", rangeHeader, err)
			}

			// Accept either 206 Partial Content or 200 OK (some servers ignore Range)
			if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
				_ = resp.Body.Close()
				return "", fmt.Errorf("unexpected status for ranged request: %s", resp.Status)
			}

			// Write chunk at correct offset
			if _, err := out.Seek(offset, 0); err != nil {
				_ = resp.Body.Close()
				return "", fmt.Errorf("failed to seek in temp file: %w", err)
			}

			written, werr := io.Copy(out, resp.Body)
			_ = resp.Body.Close()
			if werr != nil {
				return "", fmt.Errorf("failed to write chunk %s: %w", rangeHeader, werr)
			}

			// If server returned 200 and wrote less than expected, adjust offsets accordingly
			if written == 0 {
				// nothing written — avoid infinite loop
				return "", fmt.Errorf("zero bytes written for range %s", rangeHeader)
			}

			offset += written
		}

		// Close file before rename (deferred close already)
		if err := os.Rename(tempPath, fileName); err != nil {
			return "", fmt.Errorf("failed to rename temp file: %w", err)
		}
		return fileName, nil
	}

	// Fallback: single-request download
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create the request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("the request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code received: %d", resp.StatusCode)
	}

	tmp := tempPath
	outFile, err := os.Create(tmp)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	_, copyErr := io.Copy(outFile, resp.Body)
	_ = outFile.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("failed to write to file: %w", copyErr)
	}

	if err := os.Rename(tmp, fileName); err != nil {
		return "", fmt.Errorf("failed to rename temp file: %w", err)
	}

	return fileName, nil
}
