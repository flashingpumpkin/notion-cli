# notion-cli

A Golang CLI tool and GitHub Action that syncs Markdown files to Notion.

> **Note**: This entire project was vibe coded. No line of code was written by a human.

## Features

- **Sync Individual Files**: Sync a single markdown file directly to a Notion page
- **Sync Directories**: Recursively sync entire directories to a Notion database with subdirectories as inline databases
- **Index Pages**: Use `index.md` or `README.md` files to populate directory pages in Notion
- **Smart Updates**: Content hashing skips unchanged files for fast re-syncs
- **Parallel Syncing**: Files sync in parallel for improved performance
- **Internal Links**: Links like `[text](./other-file.md)` become Notion page mentions for in-app navigation
- **Stateless Operation**: No local files needed - page tracking via Notion database properties
- **Cleanup Support**: Optionally delete orphaned Notion pages without corresponding markdown files
- **Rich Formatting**: Headings, paragraphs, lists (nested, numbered, task lists), code blocks, tables, blockquotes, images, dividers, and text formatting (bold, italic, strikethrough, inline code, links)

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
2. Copy your integration token (starts with `secret_` or `ntn_`)
3. For single file sync: create or use an existing Notion page and share it with your integration
4. For directory sync: create a Notion database and share it with your integration
5. Get the page ID or database ID from the URL

### Sync a Single File

Single file sync writes markdown content directly to a Notion page (replacing existing content).

```bash
# Sync a markdown file to a Notion page
./notion-cli sync \
  --file ./my-document.md \
  --token YOUR_NOTION_TOKEN \
  --root YOUR_PAGE_ID
```

### Sync a Directory

Directory sync creates database entries for each markdown file in the specified Notion database.

```bash
# Sync an entire directory of markdown files to a Notion database
./notion-cli sync \
  --file ./docs \
  --token YOUR_NOTION_TOKEN \
  --root YOUR_DATABASE_ID
```

This will:
- Create a database entry for each markdown file
- Create inline databases for subdirectories
- Use `index.md` or `README.md` (case-insensitive) to populate directory pages
- Resolve relative links between markdown files to Notion page URLs

### Directory Structure Example

```
docs/
├── README.md           # Content for the root database entry
├── api/
│   ├── index.md        # Content for the "api" entry, which contains an inline database
│   ├── users.md        # Entry in the "api" inline database
│   └── auth.md         # Entry in the "api" inline database
└── guides/
    └── getting-started.md  # Entry in the "guides" inline database
```

### Cleanup Orphaned Pages

```bash
# Sync and delete Notion entries without corresponding markdown files
./notion-cli sync \
  --file ./docs \
  --token YOUR_NOTION_TOKEN \
  --root YOUR_DATABASE_ID \
  --cleanup
```

### Using Environment Variables

You can set the Notion token via environment variable:

```bash
export NOTION_TOKEN=YOUR_NOTION_TOKEN

./notion-cli sync \
  --file ./docs \
  --root YOUR_DATABASE_ID
```

### Command Options

- `--file, -f`: Path to the markdown file or directory to sync (required)
- `--token, -t`: Notion API token (required, can use `NOTION_TOKEN` env var)
- `--root, -r`: Notion page ID (for single file) or database ID (for directory) to sync into (required)
- `--cleanup, -c`: Delete database entries that don't have corresponding markdown files (directory sync only, optional)

## How It Works

### Syncing a Single File

When you sync a markdown file to a page:
1. The CLI parses your markdown file and converts it to Notion blocks
2. Deletes all existing blocks in the target page (except child databases)
3. Appends the new blocks to the page

The page title is not modified - only the content blocks are replaced.

### Syncing a Directory

When you sync a directory:
1. Builds a link map from existing pages (for resolving relative links)
2. Syncs markdown files in parallel (up to 15 concurrent) with live progress display
3. For subdirectories, creates a database entry with an inline child database
4. Uses `index.md` or `README.md` to populate directory entry content
5. Converts relative markdown links to Notion page mentions for internal navigation

Example output:
```
Building link map from existing pages...
Syncing 48 files in parallel...
+ introduction.md (created)
+ users.md (unchanged)
+ auth.md (unchanged)
> api-reference.md            [========------------] 8/20 blocks
> getting-started.md          [================----] 16/20 blocks
Processing subdirectory: api
  + Created directory page: api
  + Created child database: api
Directory sync completed
```

The progress display shows:
- Completed files scroll up with their status (created/updated/unchanged)
- Active syncs show a progress bar in a sticky footer
- CI environments get simple line-by-line output

### Index/README Files

- **Purpose**: Populate the content of directory pages in Notion
- **File names** (case-insensitive): `index.md`, `INDEX.md`, `readme.md`, `README.md`, etc.
- **Behavior**: The first matching file found is used as the directory's page content
- **Note**: Index/README files are not created as separate child pages

### Cleanup Mode

When using the `--cleanup` flag:
1. After syncing, the tool checks all entries in the root database
2. Entries with a "Markdown File" property that don't have a corresponding markdown file or directory are archived
3. This keeps your Notion database in sync with your markdown files

Example output:
```
✓ Directory sync completed
⚠ Deleting orphaned entry 'Old Document' (no markdown file or directory: old.md)
```

## Supported Markdown Features

- **Headings**: H1-H6 (first H1 or H2 becomes the page title)
- **Paragraphs**: Regular text paragraphs
- **Lists**: Bulleted, numbered, nested, and task lists (`- [ ]` / `- [x]`)
- **Code Blocks**: Fenced code blocks with language syntax highlighting
- **Tables**: Standard markdown tables
- **Blockquotes**: Quote blocks
- **Images**: External image URLs
- **Dividers**: Horizontal rules (`---`)
- **Text Formatting**: Bold, italic, strikethrough, inline code
- **Links**: URLs and relative markdown links (e.g., `[text](./other-file.md)` becomes a Notion page mention)

## GitHub Action

You can use this as a GitHub Action to automatically sync markdown files to Notion in your CI/CD pipeline:

### Quick Start

```yaml
name: Sync to Notion

on:
  push:
    branches: [ main ]
    paths:
      - '**.md'
      - 'docs/**'

jobs:
  sync:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Sync to Notion
        uses: flashingpumpkin/notion-cli@main
        with:
          file: ./docs
          notion-token: ${{ secrets.NOTION_TOKEN }}
          root-page-id: ${{ secrets.NOTION_DATABASE_ID }}
          cleanup: true
```

### Action Inputs

| Input | Description | Required | Default |
|-------|-------------|----------|---------|
| `file` | Path to the markdown file or directory to sync | Yes | - |
| `notion-token` | Notion API token | Yes | - |
| `root-page-id` | Page ID (for single file) or database ID (for directory) | Yes | - |
| `cleanup` | Delete orphaned database entries (directory sync only) | No | `false` |

### Examples

#### Sync a single file

```yaml
- name: Sync README
  uses: flashingpumpkin/notion-cli@main
  with:
    file: ./README.md
    notion-token: ${{ secrets.NOTION_TOKEN }}
    root-page-id: ${{ secrets.NOTION_PAGE_ID }}
```

#### Sync a directory with cleanup

```yaml
- name: Sync docs directory
  uses: flashingpumpkin/notion-cli@main
  with:
    file: ./docs
    notion-token: ${{ secrets.NOTION_TOKEN }}
    root-page-id: ${{ secrets.NOTION_DATABASE_ID }}
    cleanup: true
```

#### Multiple syncs in one workflow

```yaml
- name: Sync API docs
  uses: flashingpumpkin/notion-cli@main
  with:
    file: ./docs/api
    notion-token: ${{ secrets.NOTION_TOKEN }}
    root-page-id: ${{ secrets.NOTION_API_DOCS_DATABASE_ID }}

- name: Sync Guides
  uses: flashingpumpkin/notion-cli@main
  with:
    file: ./docs/guides
    notion-token: ${{ secrets.NOTION_TOKEN }}
    root-page-id: ${{ secrets.NOTION_GUIDES_DATABASE_ID }}
```

### Setting up Secrets

1. Go to your repository settings → Secrets and variables → Actions
2. Add the following secrets:
   - `NOTION_TOKEN`: Your Notion integration token
   - `NOTION_PAGE_ID`: The ID of your target Notion page (for single file sync)
   - `NOTION_DATABASE_ID`: The ID of your target Notion database (for directory sync)

### Using CLI directly (alternative)

If you prefer to use the CLI binary directly:

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
      - uses: actions/checkout@v4

      - name: Download notion-cli
        run: |
          # Download the latest release for your platform
          curl -L https://github.com/flashingpumpkin/notion-cli/releases/latest/download/notion-cli-linux-amd64 -o notion-cli
          chmod +x notion-cli

      - name: Sync to Notion
        env:
          NOTION_TOKEN: ${{ secrets.NOTION_TOKEN }}
        run: |
          ./notion-cli sync \
            --file ./docs \
            --root ${{ secrets.NOTION_DATABASE_ID }} \
            --cleanup
```

## How Pages Are Tracked (Directory Sync)

For directory sync, the CLI uses a **stateless approach** - no local mapping files are needed. Instead:

1. Each database entry has a "Markdown File" property storing the filename (e.g., `README.md`)
2. Each entry has a "Content Hash" property for detecting changes
3. When syncing, the tool queries the database for entries with matching filenames
4. Content hashes enable fast re-syncs by skipping unchanged files

This means:
- Sync works from any environment (CI/CD, local, etc.)
- Easy to understand which markdown file corresponds to which Notion entry
- Fast incremental syncs - only changed files are updated
- Supports cleanup of orphaned entries

For single file sync, the content is always replaced - no tracking is needed.

## License

MIT License - see LICENSE file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.