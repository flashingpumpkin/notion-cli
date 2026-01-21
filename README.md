# notion-cli

A Golang CLI tool and GitHub Action that syncs Markdown files to Notion pages.

## Features

- **Sync Markdown to Notion**: Convert and sync markdown files to Notion pages
- **Smart Updates**: Automatically creates new pages or updates existing ones
- **Hierarchical Organization**: Sync pages under a specified root page in Notion
- **Persistent Mappings**: Tracks file-to-page relationships to enable updates
- **Rich Formatting**: Supports headings, paragraphs, lists, and code blocks

## Installation

### From Source

```bash
git clone https://github.com/flashingpumpkin/notion-cli.git
cd notion-cli
go build -o notion-cli .
```

### Binary

Download the latest release from the [releases page](https://github.com/flashingpumpkin/notion-cli/releases).

## Usage

### Prerequisites

1. Create a Notion integration at https://www.notion.so/my-integrations
2. Copy your integration token (starts with `secret_`)
3. Share your target root page with your integration
4. Get the root page ID from the page URL

### Basic Usage

```bash
# Sync a markdown file to Notion
./notion-cli sync \
  --file ./my-document.md \
  --token YOUR_NOTION_TOKEN \
  --root YOUR_ROOT_PAGE_ID
```

### Using Environment Variables

You can set the Notion token via environment variable:

```bash
export NOTION_TOKEN=YOUR_NOTION_TOKEN

./notion-cli sync \
  --file ./my-document.md \
  --root YOUR_ROOT_PAGE_ID
```

### Command Options

- `--file, -f`: Path to the markdown file to sync (required)
- `--token, -t`: Notion API token (required, can use `NOTION_TOKEN` env var)
- `--root, -r`: Root page ID under which to sync the markdown page (required)
- `--database, -d`: Database ID to store page mappings (optional, defaults to file-based mapping)

## How It Works

### First Sync (Create)

When you sync a markdown file for the first time:
1. The CLI parses your markdown file
2. Creates a new Notion page under the specified root page
3. Stores the mapping in `.notion-sync-mappings.json` in the same directory as your markdown file
4. Returns the new page ID

Example output:
```
✓ Created new Notion page: abc123-def456-ghi789
```

### Subsequent Syncs (Update)

When you sync the same markdown file again:
1. The CLI reads the mapping from `.notion-sync-mappings.json`
2. Updates the existing Notion page with the new content
3. Preserves the page hierarchy

Example output:
```
✓ Updated existing Notion page: abc123-def456-ghi789
```

## Supported Markdown Features

- **Headings**: H1-H6 (H1 becomes the page title)
- **Paragraphs**: Regular text paragraphs
- **Lists**: Both ordered and unordered lists
- **Code Blocks**: Fenced code blocks with language support

## Example Markdown

```markdown
# My Document Title

This is a paragraph with some text.

## Section 1

- Bullet point 1
- Bullet point 2

## Section 2

1. Numbered item 1
2. Numbered item 2

### Code Example

```go
func main() {
    fmt.Println("Hello, Notion!")
}
```
```

## GitHub Action

You can use this tool in GitHub Actions to automatically sync markdown files to Notion:

```yaml
name: Sync to Notion

on:
  push:
    branches: [ main ]
    paths:
      - '**.md'

jobs:
  sync:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Setup Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      
      - name: Install notion-cli
        run: |
          git clone https://github.com/flashingpumpkin/notion-cli.git
          cd notion-cli
          go build -o notion-cli .
          sudo mv notion-cli /usr/local/bin/
      
      - name: Sync to Notion
        env:
          NOTION_TOKEN: ${{ secrets.NOTION_TOKEN }}
        run: |
          notion-cli sync \
            --file ./README.md \
            --root ${{ secrets.NOTION_ROOT_PAGE_ID }}
```

## File Mapping

The CLI maintains a mapping file (`.notion-sync-mappings.json`) to track which markdown files correspond to which Notion pages. This file is created in the same directory as your markdown file.

Example mapping file:
```json
{
  "abc123...": "notion-page-id-1",
  "def456...": "notion-page-id-2"
}
```

**Note**: Commit this file to your repository if you want to preserve mappings across different environments.

## License

MIT License - see LICENSE file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.