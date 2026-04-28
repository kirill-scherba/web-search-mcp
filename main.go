package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/mark3labs/mcp-go/server"
)

func main() {
	// Command line flags
	dbPath := flag.String("db", "", "Path to the SQLite database (default: ~/.config/web-search-mcp/web_search.db)")
	ollamaURL := flag.String("ollama-url", "http://localhost:11434", "Ollama API URL")
	embeddingModel := flag.String("embedding-model", defaultEmbeddingModel, "Embedding model name")
	chromiumPath := flag.String("chromium-path", "", "Path to Chromium executable (default: auto-detect)")
	showHelp := flag.Bool("h", false, "Show help")
	flag.Parse()

	if *showHelp {
		fmt.Fprintf(os.Stderr, "Usage: web-search-mcp [options]\n\n")
		fmt.Fprintf(os.Stderr, "MCP server for web search with semantic analysis.\n")
		fmt.Fprintf(os.Stderr, "Communicates via JSON-RPC 2.0 over stdin/stdout.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "Environment variables:\n")
		fmt.Fprintf(os.Stderr, "  CHROME_PATH        Path to Chromium executable\n")
		fmt.Fprintf(os.Stderr, "  OLLAMA_BASE_URL     Ollama API URL (default: http://localhost:11434)\n")
		os.Exit(0)
	}

	// Override from environment variables
	if envURL := os.Getenv("OLLAMA_BASE_URL"); envURL != "" {
		*ollamaURL = envURL
	}
	if envChrome := os.Getenv("CHROME_PATH"); envChrome != "" {
		*chromiumPath = envChrome
	}

	log.Printf("🚀 Starting web-search-mcp server")
	log.Printf("   Embedding model: %s", *embeddingModel)
	log.Printf("   Ollama URL: %s", *ollamaURL)
	log.Printf("   DB path: %s", *dbPath)
	if *chromiumPath != "" {
		log.Printf("   Chromium path: %s", *chromiumPath)
	}

	// Initialize database
	store := NewStore(*dbPath)
	if store != nil && store.Enabled() {
		defer store.Close()
	}

	// Initialize embedder
	embedder := NewEmbedder(*embeddingModel)

	// Create MCP server
	s := server.NewMCPServer(
		"web-search-mcp",
		"0.1.0",
		server.WithInstructions(`Web Search MCP - search the web with semantic analysis.

Available tools:
- web_search: Search DuckDuckGo for URLs
- web_search_analyze: Search, fetch, and rank pages by semantic relevance
- web_fetch: Fetch a single page with JS rendering
- web_semantic_search: Search previously indexed pages`),
	)

	// Register tools
	tools := []server.ServerTool{}

	// web_search
	ws, err := webSearchTool()
	if err != nil {
		log.Fatalf("Failed to create web_search tool: %v", err)
	}
	tools = append(tools, ws)

	// web_fetch
	tools = append(tools, webFetchTool(store))

	// web_search_analyze
	tools = append(tools, webSearchAnalyzeTool(embedder, store))

	// web_semantic_search
	tools = append(tools, webSemanticSearchTool(embedder, store))

	// Register all tools
	s.AddTools(tools...)

	log.Printf("✅ Registered %d tools", len(tools))

	// Start the server over stdin/stdout
	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}