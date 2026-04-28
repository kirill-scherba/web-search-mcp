package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// webSearchTool provides simple web search (returns URLs only).
func webSearchTool() (server.ServerTool, error) {
	opt := mcp.NewTool("web_search",
		mcp.WithDescription("Search the web using DuckDuckGo. Returns a list of URLs, titles and snippets. Use this to find relevant web pages before fetching them."),
		mcp.WithString("query",
			mcp.Description("The search query"),
			mcp.Required(),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of results (default: 10, max: 20)"),
		),
	)

	return server.ServerTool{
		Tool: opt,
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := request.GetArguments()
			query, _ := args["query"].(string)
			if query == "" {
				return mcp.NewToolResultText("Error: query is required"), nil
			}

			limit := 10
			if v, ok := args["limit"].(float64); ok {
				limit = int(v)
			}

			results, err := searchDDG(query, limit)
			if err != nil {
				return mcp.NewToolResultText(fmt.Sprintf("Search error: %v", err)), nil
			}

			if len(results) == 0 {
				return mcp.NewToolResultText("No results found."), nil
			}

			resultJSON, _ := json.MarshalIndent(results, "", "  ")
			return mcp.NewToolResultText(string(resultJSON)), nil
		},
	}, nil
}