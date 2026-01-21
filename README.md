# notion-cli

A Golang CLI tool and GitHub Action that syncs Markdown files to Notion pages.

## Features

- **Sync Individual Files**: Convert and sync single markdown files to Notion pages
- **Sync Directories**: Recursively sync entire directories of markdown files with nested structure
- **Index Pages**: Use `index.md` or `README.md` files to populate directory pages in Notion
- **Smart Updates**: Automatically creates new pages or updates existing ones based on filename
- **Hierarchical Organization**: Maintains directory structure in Notion page hierarchy
- **Stateless Operation**: No local files needed - page tracking via Notion page properties
- **Cleanup Support**: Optionally delete orphaned Notion pages without corresponding markdown files
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

### Sync a Single File

```bash
# Sync a markdown file to Notion
./notion-cli sync \
  --file ./my-document.md \
  --token YOUR_NOTION_TOKEN \
  --root YOUR_ROOT_PAGE_ID
```

### Sync a Directory

```bash
# Sync an entire directory of markdown files
./notion-cli sync \
  --file ./docs \
  --token YOUR_NOTION_TOKEN \
  --root YOUR_ROOT_PAGE_ID
```

This will:
- Create a Notion page for each subdirectory
- Use `index.md` or `README.md` (case-insensitive) to populate directory pages
- Sync all `.md` files while maintaining the directory hierarchy

### Directory Structure Example

```
docs/
├── README.md           # Populates the root "docs" page
├── api/
│   ├── index.md        # Populates the "api" page
│   ├── users.md        # Creates "users" child page under "api"
│   └── auth.md         # Creates "auth" child page under "api"
└── guides/
    └── getting-started.md  # Creates "getting-started" page under "guides"
```

### Cleanup Orphaned Pages

```bash
# Sync and delete Notion pages without corresponding markdown files
./notion-cli sync \
  --file ./docs \
  --token YOUR_NOTION_TOKEN \
  --root YOUR_ROOT_PAGE_ID \
  --cleanup
```

### Using Environment Variables

You can set the Notion token via environment variable:

```bash
export NOTION_TOKEN=YOUR_NOTION_TOKEN

./notion-cli sync \
  --file ./docs \
  --root YOUR_ROOT_PAGE_ID
```

### Command Options

- `--file, -f`: Path to the markdown file or directory to sync (required)
- `--token, -t`: Notion API token (required, can use `NOTION_TOKEN` env var)
- `--root, -r`: Root page ID under which to sync the markdown page (required)
- `--cleanup, -c`: Delete Notion pages that don't have corresponding markdown files (optional)

## How It Works

### Syncing a Single File

When you sync a markdown file for the first time:
1. The CLI parses your markdown file
2. Looks for an existing page with the same filename under the root page
3. If not found, creates a new Notion page under the specified root page
4. Stores the filename in a "Markdown File" property on the page
5. Returns the new page ID

Example output:
```
✓ Created new Notion page: abc123-def456-ghi789
```

When you sync the same file again:
1. The CLI looks for an existing page with the matching filename
2. Updates the existing Notion page with the new content
3. Preserves the page hierarchy

Example output:
```
✓ Updated existing Notion page: abc123-def456-ghi789
```

### Syncing a Directory

When you sync a directory:
1. The CLI recursively walks through all subdirectories
2. For each directory, it checks for `index.md` or `README.md` (case-insensitive)
3. If found, uses that file's content to populate the directory's Notion page
4. If not found, creates an empty page for the directory
5. Syncs all other `.md` files as child pages
6. Maintains the full directory hierarchy in Notion

Example output:
```
Syncing directory 'docs' using index file 'README.md'
Syncing file: introduction.md
  ✓ Created
Syncing directory 'api' using index file 'index.md'
Syncing file: users.md
  ✓ Created
Syncing file: auth.md
  ✓ Updated
✓ Directory sync completed
```

### Index/README Files

- **Purpose**: Populate the content of directory pages in Notion
- **File names** (case-insensitive): `index.md`, `INDEX.md`, `readme.md`, `README.md`, etc.
- **Behavior**: The first matching file found is used as the directory's page content
- **Note**: Index/README files are not created as separate child pages

### Cleanup Mode

When using the `--cleanup` flag:
1. After syncing, the tool checks all child pages under the root
2. Pages with a "Markdown File" property that don't have a corresponding markdown file or directory are deleted
3. This keeps your Notion workspace in sync with your markdown files

Example output:
```
✓ Updated existing Notion page: abc123-def456-ghi789
⚠ Deleting orphaned page 'Old Document' (no markdown file: old.md)
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

## How Pages Are Tracked

The CLI uses a **stateless approach** - no local mapping files are needed. Instead:

1. Each Notion page has a "Markdown File" property that stores the filename (e.g., `README.md`)
2. When syncing, the tool looks for existing pages with matching filenames
3. This allows the sync to work from any machine without needing to store state locally

This means:
- ✅ No `.notion-sync-mappings.json` file to commit
- ✅ Sync works from any environment
- ✅ Easy to understand which markdown file corresponds to which Notion page
- ✅ Supports cleanup of orphaned pages

## License

MIT License - see LICENSE file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.