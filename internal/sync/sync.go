package sync

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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
	// Check if the path is a directory or file
	info, err := os.Stat(s.config.FilePath)
	if err != nil {
		return "", false, fmt.Errorf("failed to access path: %w", err)
	}

	if info.IsDir() {
		// Sync directory
		err := s.SyncDirectory(s.config.FilePath)
		if err != nil {
			return "", false, err
		}
		// Return empty for directory sync (multiple pages)
		return "", false, nil
	}

	// Sync single file
	return s.syncFile(s.config.FilePath, s.config.RootPageID, "")
}

// SyncDirectory syncs a directory of markdown files to Notion
func (s *Syncer) SyncDirectory(dirPath string) error {
	// Verify the directory exists
	info, err := os.Stat(dirPath)
	if err != nil {
		return fmt.Errorf("failed to access directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", dirPath)
	}

	// Sync the directory recursively
	return s.syncDirectoryRecursive(dirPath, s.config.RootPageID, dirPath)
}

// syncDirectoryRecursive recursively syncs a directory to Notion
func (s *Syncer) syncDirectoryRecursive(currentPath, parentPageID, basePath string) error {
	// Read directory contents
	entries, err := os.ReadDir(currentPath)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	// Check for index/readme files in this directory
	var indexFile string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lowerName := strings.ToLower(name)
		if lowerName == "index.md" || lowerName == "readme.md" {
			indexFile = filepath.Join(currentPath, name)
			break
		}
	}

	// If this is not the base directory, we need to create/find a page for it
	currentPageID := parentPageID
	if currentPath != basePath {
		// Use the directory name as the page identifier
		dirName := filepath.Base(currentPath)
		
		if indexFile != "" {
			// Sync the index file to represent this directory
			fmt.Printf("Syncing directory '%s' using index file '%s'\n", dirName, filepath.Base(indexFile))
			pageID, _, err := s.syncFile(indexFile, parentPageID, dirName)
			if err != nil {
				return fmt.Errorf("failed to sync index file: %w", err)
			}
			currentPageID = pageID
		} else {
			// Create or find an empty page for this directory
			fmt.Printf("Creating/finding page for directory '%s'\n", dirName)
			pageID, _, err := s.findOrCreateDirectoryPage(parentPageID, dirName)
			if err != nil {
				return fmt.Errorf("failed to create directory page: %w", err)
			}
			currentPageID = pageID
		}
	}

	// Process all markdown files in this directory
	for _, entry := range entries {
		entryPath := filepath.Join(currentPath, entry.Name())
		
		if entry.IsDir() {
			// Recursively sync subdirectory
			err := s.syncDirectoryRecursive(entryPath, currentPageID, basePath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to sync directory %s: %v\n", entryPath, err)
			}
		} else if strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			// Skip index files as they've already been processed
			if indexFile != "" && entryPath == indexFile {
				continue
			}
			
			// Sync the markdown file
			fmt.Printf("Syncing file: %s\n", entry.Name())
			_, created, err := s.syncFile(entryPath, currentPageID, "")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to sync file %s: %v\n", entryPath, err)
			} else if created {
				fmt.Printf("  ✓ Created\n")
			} else {
				fmt.Printf("  ✓ Updated\n")
			}
		}
	}

	return nil
}

// syncFile syncs a single file to a parent page, optionally using a custom identifier
func (s *Syncer) syncFile(filePath, parentPageID, customIdentifier string) (string, bool, error) {
	// Read the markdown file
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", false, fmt.Errorf("failed to read file: %w", err)
	}

	// Parse markdown to extract title and content
	doc, err := markdown.Parse(content)
	if err != nil {
		return "", false, fmt.Errorf("failed to parse markdown: %w", err)
	}

	// Determine the identifier (filename or custom)
	var identifier string
	if customIdentifier != "" {
		identifier = customIdentifier
	} else {
		identifier = filepath.Base(filePath)
	}

	// Look up if a child page exists with this identifier
	pageID, exists, err := s.notionClient.FindPageByFilename(parentPageID, identifier)
	if err != nil {
		return "", false, fmt.Errorf("failed to lookup page by filename: %w", err)
	}

	if exists {
		// Update existing page
		err = s.notionClient.UpdatePage(pageID, doc.Title, identifier, doc.Blocks)
		if err != nil {
			return "", false, fmt.Errorf("failed to update page: %w", err)
		}
		return pageID, false, nil
	}

	// Create new page
	pageID, err = s.notionClient.CreatePage(parentPageID, doc.Title, identifier, doc.Blocks)
	if err != nil {
		return "", false, fmt.Errorf("failed to create page: %w", err)
	}

	return pageID, true, nil
}

// findOrCreateDirectoryPage finds or creates a page for a directory
func (s *Syncer) findOrCreateDirectoryPage(parentPageID, dirName string) (string, bool, error) {
	// Look for existing page
	pageID, exists, err := s.notionClient.FindPageByFilename(parentPageID, dirName)
	if err != nil {
		return "", false, err
	}

	if exists {
		return pageID, false, nil
	}

	// Create new empty page
	// Use dirName for both title and filename property (for tracking)
	pageID, err = s.notionClient.CreatePage(parentPageID, dirName, dirName, []notionapi.Block{})
	if err != nil {
		return "", false, err
	}

	return pageID, true, nil
}

// CleanupOrphanedPages deletes Notion pages that don't have corresponding markdown files
func (s *Syncer) CleanupOrphanedPages(paths []string) error {
	// Build a set of all markdown files and directories
	fileSet := make(map[string]bool)
	
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		
		if info.IsDir() {
			// Walk directory and collect all markdown files and directories
			err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil // Skip errors
				}
				
				if d.IsDir() {
					// Add directory name
					if p != path { // Skip the root path itself
						fileSet[filepath.Base(p)] = true
					}
				} else if strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
					fileSet[d.Name()] = true
				}
				
				return nil
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to walk directory %s: %v\n", path, err)
			}
		} else {
			// Add single file
			fileSet[filepath.Base(path)] = true
		}
	}

	// Get all child pages from the root
	pages, err := s.notionClient.GetChildPages(s.config.RootPageID)
	if err != nil {
		return fmt.Errorf("failed to get child pages: %w", err)
	}

	// Check each page
	for _, page := range pages {
		// Get the "Markdown File" property
		if prop, ok := page.Properties["Markdown File"]; ok {
			if richTextProp, ok := prop.(*notionapi.RichTextProperty); ok {
				if len(richTextProp.RichText) > 0 {
					pageFilename := richTextProp.RichText[0].PlainText
					// If the markdown file or directory doesn't exist, delete the page
					if !fileSet[pageFilename] {
						fmt.Printf("⚠ Deleting orphaned page '%s' (no markdown file or directory: %s)\n", getPageTitle(page), pageFilename)
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
