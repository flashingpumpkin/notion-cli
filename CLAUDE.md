# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

This project was entirely vibe coded - no line of code was written by a human. It syncs Markdown files into Notion databases only (not pages).

## Build and Test Commands

```bash
go build .              # Build the binary
go install .            # Install to $GOPATH/bin
go test ./...           # Run all tests
go test -v ./...        # Run tests with verbose output
go test ./internal/markdown/...  # Run tests for a specific package
```

## Architecture

This is a Go CLI tool that syncs Markdown files to Notion databases. It uses the `urfave/cli` framework for command parsing and `jomei/notionapi` for Notion API interactions.

### Package Structure

**main.go** - CLI entry point using urfave/cli. Defines the `sync` command with flags for file path, token, root page ID, and cleanup mode.

**internal/sync/** - Orchestrates the sync process:
- Handles both single file and directory syncing
- Parallel file syncing with configurable concurrency (maxParallelSyncs = 15)
- Interactive progress display with sticky footer showing active syncs
- CI detection for fallback to simple line output
- Builds link map upfront from existing pages, resolves links during initial sync (single pass)
- Content hashing to skip unchanged files
- Subdirectories become nested inline databases with their own child databases

**internal/notion/** - Notion API client wrapper:
- Database entry CRUD operations with batching (maxBlocksPerRequest = 100)
- Smart block updates: updates blocks in place where types match, only deletes/appends when necessary
- Property management with caching to prevent duplicate property creation during parallel operations
- Tracks pages via "Markdown File" and "Content Hash" properties

**internal/markdown/** - Markdown parsing using goldmark:
- Converts markdown AST to Notion blocks
- Supports: headings, paragraphs, lists (bulleted, numbered, nested, task lists), code blocks, blockquotes, tables, images, dividers
- Rich text formatting: bold, italic, strikethrough, inline code, links
- Title extraction: uses first H1, falls back to first H2, defaults to "Untitled"
- ResolveRelativeLinks converts `./file.md` links to Notion page mentions for internal navigation

### Sync Flow

1. Build link map from existing database entries (for resolving relative links)
2. For directories: recursively walk, using index.md/README.md to populate directory pages
3. Create/update database entries with content hash for change detection
4. Links are resolved during parsing using the pre-built link map (single pass)
5. Subdirectories create a page entry in parent database, then an inline child database within that page

### Key Constants

- `maxBlocksPerRequest = 100` - Notion API limit per request
- `maxParallelSyncs = 15` - Concurrent file sync limit
- `maxConcurrent = 20` - Parallel block deletion/update limit
