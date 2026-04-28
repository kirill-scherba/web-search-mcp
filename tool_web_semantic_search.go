package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// webSemanticSearchTool searches previously indexed pages by semantic relevance.
func webSemanticSearchTool(embedder *Embedder, store *Store) server.ServerTool {
	opt := mcp.NewTool("web_semantic_search",
		mcp.WithDescription("Search previously indexed web pages by semantic relevance. Use this to find relevant content from pages that were already fetched and indexed by web_search_analyze."),
		mcp.WithString("query",
			mcp.Description("The semantic search query"),
			mcp.Required(),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of results (default: 5, max: 20)"),
		),
		mcp.WithString("model",
			mcp.Description("Embedding model name (optional, default: embeddinggemma:latest)"),
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

			if store == nil || !store.Enabled() {
				return mcp.NewToolResultText("Error: database is not available. No indexed pages found."), nil
			}

			if embedder == nil || !embedder.Ready() {
				return mcp.NewToolResultText("Error: embedding search is not available. Please ensure Ollama is running with the embedding model."), nil
			}

			limit := 5
			if v, ok := args["limit"].(float64); ok {
				limit = int(v)
			}
			if limit > 20 {
				limit = 20
			}

			// Generate embedding for the query
			queryEmbedding, err := embedder.GenerateEmbedding(query)
			if err != nil {
				return mcp.NewToolResultText(fmt.Sprintf("Failed to generate query embedding: %v", err)), nil
			}

			// Search by semantic relevance
			results, err := store.SearchSemantic(queryEmbedding, limit)
			if err != nil {
				return mcp.NewToolResultText(fmt.Sprintf("Search error: %v", err)), nil
			}

			if len(results) == 0 {
				return mcp.NewToolResultText("No relevant content found in indexed pages. Try using web_search_analyze first to fetch and index pages."), nil
			}

			resultJSON, _ := json.MarshalIndent(results, "", "  ")
			return mcp.NewToolResultText(string(resultJSON)), nil
		},
	}
}