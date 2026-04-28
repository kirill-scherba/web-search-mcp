# Context

## Project Overview

web-search-mcp is an MCP (Model Context Protocol) server that provides web search capabilities with semantic analysis. It allows AI assistants to search the web, fetch and render JavaScript-heavy pages, extract readable content, analyze pages by semantic relevance using embeddings, and search previously indexed content.

## Key Features

- **DuckDuckGo Search**: Free, no API key required
- **JavaScript Rendering**: Uses chromedp to render SPAs and JS-heavy pages
- **Content Extraction**: Mozilla Readability algorithm for clean article text
- **Semantic Analysis**: Embeddings via Ollama (embeddinggemma:latest) to rank pages by relevance
- **Persistent Cache**: Pages and embeddings stored in libSQL for reuse
- **4 MCP Tools**: web_search, web_search_analyze, web_fetch, web_semantic_search

## Architecture

```
┌──────────────────────────────────────────────────────┐
│                     MCP Client (AI)                   │
│  initialize → tools/list → tools/call                │
└──────────────────────────┬───────────────────────────┘
                           │ JSON-RPC 2.0
                           │ stdin/stdout
┌──────────────────────────▼───────────────────────────┐
│                  web-search-mcp (Go)                   │
│                                                       │
│  ┌──────────────┐   ┌──────────────┐                  │
│  │  MCP Server  │──→│  Tools       │                  │
│  │  (mcp-go)    │   │  ┌────────┐ │                  │
│  └──────────────┘   │  │search  │ │─→ DuckDuckGo     │
│                      │  ├────────┤ │                  │
│                      │  │fetch   │ │─→ chromedp       │
│                      │  ├────────┤ │   + readability  │
│                      │  │analyze │ │─→ search + fetch │
│                      │  ├────────┤ │   + embedding    │
│                      │  │semantic│ │─→ vector search  │
│                      │  └────────┘ │                  │
│                      └──────────────┘                  │
│                           │                            │
│                      ┌────▼────┐                      │
│                      │ Ollama  │                      │
│                      │embedding│                      │
│                      └─────────┘                      │
│                           │                            │
│                      ┌────▼────┐                      │
│                      │ libSQL  │                      │
│                      │ (cache) │                      │
│                      └─────────┘                      │
└──────────────────────────────────────────────────────┘
```

## Dependencies

- Go 1.26+
- Ollama with embedding model (embeddinggemma:latest)
- Chromium/Chrome (for chromedp)
- libSQL (go-libsql driver)