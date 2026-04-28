package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// analyzedResult represents a search result with relevance score.
type analyzedResult struct {
	URL       string  `json:"url"`
	Title     string  `json:"title"`
	Relevance float64 `json:"relevance"`
	Snippet   string  `json:"snippet"`
}

// webSearchAnalyzeTool searches, fetches, and ranks results by semantic relevance.
func webSearchAnalyzeTool(embedder *Embedder, store *Store) server.ServerTool {
	opt := mcp.NewTool("web_search_analyze",
		mcp.WithDescription("Search the web, fetch pages, and rank them by semantic relevance to the query. Uses embeddings to compare page content with the query. Returns results sorted by relevance percentage."),
		mcp.WithString("query",
			mcp.Description("The search query"),
			mcp.Required(),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of pages to analyze (default: 8, max: 15)"),
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

			limit := 8
			if v, ok := args["limit"].(float64); ok {
				limit = int(v)
			}
			if limit > 15 {
				limit = 15
			}

			// Check embedder availability
			if embedder == nil || !embedder.Ready() {
				return mcp.NewToolResultText("Error: embedding search is not available. Please ensure Ollama is running with the embedding model."), nil
			}

			// Step 1: Search
			searchResults, err := searchDDG(query, limit)
			if err != nil {
				return mcp.NewToolResultText(fmt.Sprintf("Search error: %v", err)), nil
			}

			if len(searchResults) == 0 {
				return mcp.NewToolResultText("No results found."), nil
			}

			// Step 2: Generate embedding for the query
			queryEmbedding, err := embedder.GenerateEmbedding(query)
			if err != nil {
				return mcp.NewToolResultText(fmt.Sprintf("Failed to generate query embedding: %v", err)), nil
			}

			// Step 3: For each result, fetch the page and generate embedding
			type fetchResult struct {
				url      string
				title    string
				text     string
				embedding []float32
				err      error
			}

			var wg sync.WaitGroup
			fetchChan := make(chan fetchResult, len(searchResults))

			for _, sr := range searchResults {
				wg.Add(1)
				go func(sr SearchResult) {
					defer wg.Done()

					// Try to fetch with chromedp; fall back to simple fetch
					page, err := fetchPage(sr.URL, 3)
					if err != nil {
						// Try simple fetch as fallback
						page, err = fetchPageSimple(sr.URL)
						if err != nil {
							fetchChan <- fetchResult{url: sr.URL, title: sr.Title, err: err}
							return
						}
					}

					// Truncate long text for embedding
					text := page.Text
					const maxTextLen = 1500
					if len(text) > maxTextLen {
						text = text[:maxTextLen]
					}

					// Generate embedding
					emb, err := embedder.GenerateEmbedding(text)
					if err != nil {
						fetchChan <- fetchResult{url: sr.URL, title: page.Title, err: err}
						return
					}

					fetchChan <- fetchResult{
						url:       sr.URL,
						title:     page.Title,
						text:      text,
						embedding: emb,
					}
				}(sr)
			}

			go func() {
				wg.Wait()
				close(fetchChan)
			}()

			// Collect results
			var analyzed []analyzedResult
			for fr := range fetchChan {
				if fr.err != nil || fr.embedding == nil {
					analyzed = append(analyzed, analyzedResult{
						URL:       fr.url,
						Title:     fr.title,
						Relevance: 0,
						Snippet:   fmt.Sprintf("Failed to analyze: %v", fr.err),
					})
					continue
				}

				// Calculate relevance
				relevance := cosineSimilarity(queryEmbedding, fr.embedding)

				snippet := fr.text
				if len(snippet) > 300 {
					snippet = snippet[:300] + "..."
				}

				analyzed = append(analyzed, analyzedResult{
					URL:       fr.url,
					Title:     fr.title,
					Relevance: relevance,
					Snippet:   snippet,
				})

				// Save to database for future semantic search
				if store != nil && store.Enabled() {
					pageID, err := store.SavePage(&FetchedPage{
						URL:       fr.url,
						Title:     fr.title,
						Text:      fr.text,
						FetchedAt: 0,
					})
					if err == nil {
						// Split into chunks and save with embeddings
						chunks := chunkText(fr.text, 1000)
						for i, chunk := range chunks {
							chunkEmb, err := embedder.GenerateEmbedding(chunk)
							if err == nil {
								store.SaveChunk(pageID, fr.url, fr.title, i, chunk, chunkEmb)
							}
						}
					}
				}
			}

			if len(analyzed) == 0 {
				return mcp.NewToolResultText("No results could be analyzed."), nil
			}

			// Sort by relevance descending
			for i := 0; i < len(analyzed); i++ {
				for j := i + 1; j < len(analyzed); j++ {
					if analyzed[j].Relevance > analyzed[i].Relevance {
						analyzed[i], analyzed[j] = analyzed[j], analyzed[i]
					}
				}
			}

			resultJSON, _ := json.MarshalIndent(analyzed, "", "  ")
			return mcp.NewToolResultText(string(resultJSON)), nil
		},
	}
}