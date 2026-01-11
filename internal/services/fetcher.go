package services

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Fetcher struct {
	client *http.Client
}

func NewFetcher() *Fetcher {
	return &Fetcher{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (f *Fetcher) newRequest(ctx context.Context, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// Emulate a common browser; avoid compression/decompression mismatches
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	return req, nil
}

// FetchURL retrieves the content from a URL
func (f *Fetcher) FetchURL(ctx context.Context, url string) (string, error) {
	// Try once, and if 202, retry once after a short delay
	for attempt := 0; attempt < 2; attempt++ {
		req, err := f.newRequest(ctx, url)
		if err != nil {
			return "", fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := f.client.Do(req)
		if err != nil {
			return "", fmt.Errorf("failed to fetch URL: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return "", fmt.Errorf("failed to read response body: %w", err)
			}
			return string(body), nil
		}

		if resp.StatusCode == http.StatusAccepted && attempt == 0 {
			// 202 Accepted: retry once after a brief wait
			t := time.NewTimer(750 * time.Millisecond)
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("fetch canceled: %w", ctx.Err())
			case <-t.C:
			}
			continue
		}

		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return "", fmt.Errorf("failed to fetch URL after retries")
}
