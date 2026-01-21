package sync

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/flashingpumpkin/notion-cli/internal/markdown"
	"github.com/flashingpumpkin/notion-cli/internal/notion"
	"github.com/jomei/notionapi"
)

// Config holds the configuration for syncing
type Config struct {
	FilePath    string
	NotionToken string
	RootPageID  string
}

// Syncer handles syncing markdown files to Notion
type Syncer struct {
	config       Config
	notionClient *notion.Client
}

// NewSyncer creates a new Syncer instance
func NewSyncer(config Config) (*Syncer, error) {
	client, err := notion.NewClient(config.NotionToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create notion client: %w", err)
	}

	return &Syncer{
		config:       config,
		notionClient: client,
	}, nil
}

// Sync syncs the markdown file to Notion
// Returns: pageID, wasCreated, error
func (s *Syncer) Sync() (string, bool, error) {
	// Read the markdown file
	content, err := os.ReadFile(s.config.FilePath)
	if err != nil {
		return "", false, fmt.Errorf("failed to read file: %w", err)
	}

	// Parse markdown to extract title and content
	doc, err := markdown.Parse(content)
	if err != nil {
		return "", false, fmt.Errorf("failed to parse markdown: %w", err)
	}

	// Get the filename (not the full path)
	filename := filepath.Base(s.config.FilePath)

	// Look up if a child page exists with this filename
	pageID, exists, err := s.notionClient.FindPageByFilename(s.config.RootPageID, filename)
	if err != nil {
		return "", false, fmt.Errorf("failed to lookup page by filename: %w", err)
	}

	if exists {
		// Update existing page
		err = s.notionClient.UpdatePage(pageID, doc.Title, filename, doc.Blocks)
		if err != nil {
			return "", false, fmt.Errorf("failed to update page: %w", err)
		}
		return pageID, false, nil
	}

	// Create new page
	pageID, err = s.notionClient.CreatePage(s.config.RootPageID, doc.Title, filename, doc.Blocks)
	if err != nil {
		return "", false, fmt.Errorf("failed to create page: %w", err)
	}

	return pageID, true, nil
}

// CleanupOrphanedPages deletes Notion pages that don't have corresponding markdown files
func (s *Syncer) CleanupOrphanedPages(markdownFiles []string) error {
	// Get all child pages from the root
	pages, err := s.notionClient.GetChildPages(s.config.RootPageID)
	if err != nil {
		return fmt.Errorf("failed to get child pages: %w", err)
	}

	// Build a map of markdown filenames for quick lookup
	markdownFileSet := make(map[string]bool)
	for _, file := range markdownFiles {
		filename := filepath.Base(file)
		markdownFileSet[filename] = true
	}

	// Check each page
	for _, page := range pages {
		// Get the "Markdown File" property
		if prop, ok := page.Properties["Markdown File"]; ok {
			if richTextProp, ok := prop.(*notionapi.RichTextProperty); ok {
				if len(richTextProp.RichText) > 0 {
					pageFilename := richTextProp.RichText[0].PlainText
					// If the markdown file doesn't exist, delete the page
					if !markdownFileSet[pageFilename] {
						fmt.Printf("⚠ Deleting orphaned page '%s' (no markdown file: %s)\n", getPageTitle(page), pageFilename)
						err = s.notionClient.DeletePage(string(page.ID))
						if err != nil {
							fmt.Fprintf(os.Stderr, "Warning: failed to delete page %s: %v\n", page.ID, err)
						}
					}
				}
			}
		}
	}

	return nil
}

// getPageTitle extracts the title from a page
func getPageTitle(page notionapi.Page) string {
	// Try different common title property names
	titleProps := []string{"title", "Title", "Name", "name"}
	
	for _, propName := range titleProps {
		if prop, ok := page.Properties[propName]; ok {
			if titleProp, ok := prop.(*notionapi.TitleProperty); ok {
				if len(titleProp.Title) > 0 {
					return titleProp.Title[0].PlainText
				}
			}
		}
	}
	
	return "Untitled"
}
