package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

// SearchResult represents a single search result.
type SearchResult struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

// searchDDG searches DuckDuckGo and returns search results.
func searchDDG(query string, limit int) ([]SearchResult, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}

	// Build the DuckDuckGo HTML search URL
	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))

	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers to mimic a real browser
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", "https://duckduckgo.com/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to search DuckDuckGo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DuckDuckGo returned status %d", resp.StatusCode)
	}

	// Parse the HTML response
	results, err := parseDDGResults(resp.Body, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to parse search results: %w", err)
	}

	return results, nil
}

// parseDDGResults parses DuckDuckGo HTML search results.
func parseDDGResults(r io.Reader, limit int) ([]SearchResult, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, err
	}

	var results []SearchResult

	// DuckDuckGo HTML results are in <div class="result"> elements
	resultSelector := cascadia.MustCompile("div.result")
	resultNodes := resultSelector.MatchAll(doc)

	for _, node := range resultNodes {
		if len(results) >= limit {
			break
		}

		result := SearchResult{}

		// Extract URL: <a class="result__a"> or <a> inside result
		linkSelector := cascadia.MustCompile("a.result__a, a")
		linkNode := linkSelector.MatchFirst(node)
		if linkNode != nil {
			result.Title = extractText(linkNode)
			result.URL = extractAttr(linkNode, "href")
			// Clean up the URL (DDG wraps it in redirect)
			result.URL = cleanDDGURL(result.URL)
		}

		// Extract snippet: <a class="result__snippet"> or <span class="result__snippet">
		snippetSelector := cascadia.MustCompile(".result__snippet, .result__snippet a")
		snippetNode := snippetSelector.MatchFirst(node)
		if snippetNode != nil {
			result.Snippet = extractText(snippetNode)
		}

		// Skip empty results
		if result.URL == "" || result.Title == "" {
			continue
		}

		results = append(results, result)
	}

	// If no results found with the standard selector, try alternative parsing
	if len(results) == 0 {
		results = parseDDGResultsFallback(doc, limit)
	}

	return results, nil
}

// parseDDGResultsFallback tries alternative parsing for DuckDuckGo results.
func parseDDGResultsFallback(doc *html.Node, limit int) []SearchResult {
	var results []SearchResult

	// Look for links with result-like classes
	var findLinks func(*html.Node)
	findLinks = func(n *html.Node) {
		if len(results) >= limit {
			return
		}
		if n.Type == html.ElementNode && n.Data == "a" {
			classes := getAttr(n, "class")
			href := getAttr(n, "href")
			if strings.Contains(classes, "result") && href != "" {
				title := extractText(n)
				if title != "" {
					result := SearchResult{
						URL:   cleanDDGURL(href),
						Title: title,
					}
					// Find sibling snippet
					for sibling := n.Parent.FirstChild; sibling != nil; sibling = sibling.NextSibling {
						if sibling.Type == html.ElementNode {
							sibClasses := getAttr(sibling, "class")
							if strings.Contains(sibClasses, "snippet") {
								result.Snippet = extractText(sibling)
								break
							}
						}
					}
					results = append(results, result)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findLinks(c)
		}
	}
	findLinks(doc)

	return results
}

// extractText extracts all text content from a node.
func extractText(n *html.Node) string {
	var sb strings.Builder
	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(n)
	return strings.TrimSpace(sb.String())
}

// extractAttr returns the value of an attribute from a node.
func extractAttr(n *html.Node, name string) string {
	for _, attr := range n.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

// getAttr returns the value of an attribute from a node.
func getAttr(n *html.Node, name string) string {
	return extractAttr(n, name)
}

// cleanDDGURL removes DuckDuckGo redirect wrapper from URLs.
func cleanDDGURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	// DDG sometimes wraps URLs like: //duckduckgo.com/l/?uddg=REAL_URL
	if strings.Contains(rawURL, "uddg=") {
		parsed, err := url.Parse(rawURL)
		if err == nil {
			decoded := parsed.Query().Get("uddg")
			if decoded != "" {
				return decoded
			}
		}
	}

	// Handle protocol-relative URLs
	if strings.HasPrefix(rawURL, "//") {
		rawURL = "https:" + rawURL
	}

	return rawURL
}