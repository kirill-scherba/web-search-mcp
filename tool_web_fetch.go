package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// webFetchTool creates a tool that fetches a web page and extracts readable content.
func webFetchTool(store *Store) server.ServerTool {
	opt := mcp.NewTool("web_fetch",
		mcp.WithDescription("Fetch a web page, render JavaScript, and extract readable content. Returns the full text of the page. Use this to get detailed content from a URL found via web_search."),
		mcp.WithString("url",
			mcp.Description("The URL to fetch"),
			mcp.Required(),
		),
		mcp.WithNumber("wait_time",
			mcp.Description("Time in seconds to wait for JavaScript rendering (default: 3)"),
		),
	)

	return server.ServerTool{
		Tool: opt,
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := request.GetArguments()
			pageURL, _ := args["url"].(string)
			if pageURL == "" {
				return mcp.NewToolResultText("Error: url is required"), nil
			}

			waitTime := 3
			if v, ok := args["wait_time"].(float64); ok {
				waitTime = int(v)
			}

			// Check cache first
			if store != nil && store.Enabled() {
				existing, err := store.PageExists(pageURL)
				if err == nil && existing != nil && isCacheValid(existing.FetchedAt) {
					result := map[string]interface{}{
						"url":     existing.URL,
						"title":   existing.Title,
						"text":    existing.FullText,
						"length":  len(existing.FullText),
						"cached":  true,
					}
					j, _ := json.MarshalIndent(result, "", "  ")
					return mcp.NewToolResultText(string(j)), nil
				}
			}

			// Fetch the page
			page, err := fetchPage(pageURL, waitTime)
			if err != nil {
				return mcp.NewToolResultText(fmt.Sprintf("Fetch error: %v", err)), nil
			}

			// Save to cache
			if store != nil && store.Enabled() {
				store.SavePage(page)
			}

			result := map[string]interface{}{
				"url":       page.URL,
				"title":     page.Title,
				"text":      page.Text,
				"excerpt":   page.Excerpt,
				"site_name": page.SiteName,
				"length":    page.Length,
				"cached":    false,
			}
			j, _ := json.MarshalIndent(result, "", "  ")
			return mcp.NewToolResultText(string(j)), nil
		},
	}
}