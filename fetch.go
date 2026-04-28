package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/go-shiori/go-readability"
)

// FetchedPage represents a fetched and extracted web page.
type FetchedPage struct {
	URL       string `json:"url"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Text      string `json:"text"`
	Excerpt   string `json:"excerpt"`
	SiteName  string `json:"site_name"`
	Length    int    `json:"length"`
	FetchedAt int64  `json:"fetched_at"`
}

// fetchPage fetches a URL, renders JavaScript, and extracts readable content.
// Uses chromedp for JS rendering and go-readability for content extraction.
func fetchPage(pageURL string, waitTime int) (*FetchedPage, error) {
	if waitTime <= 0 {
		waitTime = 3
	}

	// Validate URL
	parsedURL, err := url.Parse(pageURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, fmt.Errorf("invalid URL: %s", pageURL)
	}

	// Create a new context for chromedp
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", true),
			chromedp.Flag("disable-gpu", true),
			chromedp.Flag("no-sandbox", true),
			chromedp.Flag("disable-dev-shm-usage", true),
		)...,
	)
	defer allocCancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	// Navigate to the page and wait for it to load
	var htmlContent string
	err = chromedp.Run(ctx,
		chromedp.Navigate(pageURL),
		chromedp.Sleep(time.Duration(waitTime)*time.Second),
		chromedp.OuterHTML("html", &htmlContent),
	)
	if err != nil {
		return nil, fmt.Errorf("chromedp error for %s: %w", pageURL, err)
	}

	// Create an HTTP response reader for readability
	reader := io.NopCloser(strings.NewReader(htmlContent))

	// Use go-readability to extract the main content
	article, err := readability.FromReader(reader, &url.URL{
		Scheme: parsedURL.Scheme,
		Host:   parsedURL.Host,
		Path:   parsedURL.Path,
	})
	if err != nil {
		// If readability fails, return the raw HTML body as text
		return &FetchedPage{
			URL:       pageURL,
			Title:     extractTitle(htmlContent),
			Content:   htmlContent,
			Text:      htmlContent,
			Length:    len(htmlContent),
			FetchedAt: time.Now().Unix(),
		}, nil
	}

	// Clean up the text
	articleText := strings.TrimSpace(article.TextContent)
	articleTitle := strings.TrimSpace(article.Title)

	return &FetchedPage{
		URL:       pageURL,
		Title:     articleTitle,
		Content:   article.Content,
		Text:      articleText,
		Excerpt:   article.Excerpt,
		SiteName:  article.SiteName,
		Length:    len(articleText),
		FetchedAt: time.Now().Unix(),
	}, nil
}

// fetchPageSimple fetches a URL without JavaScript rendering (fallback).
func fetchPageSimple(pageURL string) (*FetchedPage, error) {
	parsedURL, err := url.Parse(pageURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, fmt.Errorf("invalid URL: %s", pageURL)
	}

	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	req, err := http.NewRequest("GET", pageURL, nil)
	if err != nil {
		return nil, err
	}

	// Use a common user-agent to avoid being blocked
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Use readability on the HTTP response
	article, err := readability.FromReader(resp.Body, parsedURL)
	if err != nil {
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)
		return &FetchedPage{
			URL:       pageURL,
			Title:     extractTitle(bodyStr),
			Text:      bodyStr,
			Length:    len(bodyStr),
			FetchedAt: time.Now().Unix(),
		}, nil
	}

	return &FetchedPage{
		URL:       pageURL,
		Title:     strings.TrimSpace(article.Title),
		Content:   article.Content,
		Text:      strings.TrimSpace(article.TextContent),
		Excerpt:   article.Excerpt,
		SiteName:  article.SiteName,
		Length:    len(article.TextContent),
		FetchedAt: time.Now().Unix(),
	}, nil
}

// extractTitle extracts the <title> from raw HTML.
func extractTitle(htmlContent string) string {
	lower := strings.ToLower(htmlContent)
	start := strings.Index(lower, "<title>")
	if start == -1 {
		return ""
	}
	start += len("<title>")
	end := strings.Index(lower[start:], "</title>")
	if end == -1 {
		return ""
	}
	return strings.TrimSpace(htmlContent[start : start+end])
}