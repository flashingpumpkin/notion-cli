package sync

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	gosync "sync"

	"github.com/flashingpumpkin/notion-cli/internal/markdown"
	"github.com/flashingpumpkin/notion-cli/internal/notion"
	"github.com/jomei/notionapi"
)

const defaultConcurrency = 15

// Config holds the configuration for syncing
type Config struct {
	FilePath    string
	NotionToken string
	RootPageID  string // This is now a database ID
	Force       bool   // Force update even if content hash matches
	Concurrency int    // Number of parallel file syncs (default 15)
	Debug       bool   // Enable debug logging
}

// getConcurrency returns the configured concurrency or the default
func (c Config) getConcurrency() int {
	if c.Concurrency <= 0 {
		return defaultConcurrency
	}
	return c.Concurrency
}

// debugf prints debug output if debug mode is enabled
func (s *Syncer) debugf(format string, args ...interface{}) {
	if s.config.Debug {
		fmt.Printf("[DEBUG] "+format+"\n", args...)
	}
}

// Syncer handles syncing markdown files to Notion
type Syncer struct {
	config       Config
	notionClient *notion.Client
}

// NewSyncer creates a new Syncer instance
func NewSyncer(config Config) (*Syncer, error) {
	client, err := notion.NewClient(config.NotionToken, config.Debug)
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
	s.debugf("Starting sync for path: %s", s.config.FilePath)
	s.debugf("Concurrency: %d, Force: %v", s.config.getConcurrency(), s.config.Force)

	// Check if the path is a directory or file
	info, err := os.Stat(s.config.FilePath)
	if err != nil {
		return "", false, fmt.Errorf("failed to access path: %w", err)
	}
	s.debugf("Path is directory: %v", info.IsDir())

	// Check if root is a page or database
	s.debugf("Checking if root ID is page or database: %s", s.config.RootPageID)
	isPage, err := s.notionClient.IsPage(s.config.RootPageID)
	if err != nil {
		return "", false, fmt.Errorf("failed to validate root ID: %w", err)
	}
	s.debugf("Root is page: %v", isPage)

	if info.IsDir() {
		// Directory sync requires a database as root
		if isPage {
			return "", false, fmt.Errorf("directory sync requires a database ID as root, but got a page ID")
		}

		fmt.Printf("Starting sync to database: %s\n", s.config.RootPageID)
		fmt.Printf("Syncing directory: %s\n", s.config.FilePath)
		err := s.SyncDirectory(s.config.FilePath)
		if err != nil {
			return "", false, err
		}
		fmt.Println("Directory sync completed")
		return "", false, nil
	}

	// Single file sync - requires a page as root
	if !isPage {
		return "", false, fmt.Errorf("single file sync requires a page ID as root, but got a database ID")
	}

	fmt.Printf("Syncing single file to page: %s\n", s.config.RootPageID)
	fmt.Printf("File: %s\n", s.config.FilePath)

	err = s.syncFileToPage(s.config.FilePath, s.config.RootPageID)
	if err != nil {
		return "", false, err
	}

	return s.config.RootPageID, false, nil
}

// syncFileToPage syncs a single markdown file directly to a page
func (s *Syncer) syncFileToPage(filePath, pageID string) error {
	// Read the markdown file
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Parse markdown
	doc, err := markdown.Parse(content)
	if err != nil {
		return fmt.Errorf("failed to parse markdown: %w", err)
	}

	// Sync blocks to the page
	fmt.Printf("  Syncing %d blocks...\n", len(doc.Blocks))
	err = s.notionClient.SyncToPage(pageID, doc.Blocks)
	if err != nil {
		return err
	}
	fmt.Println("  ✓ Synced")
	return nil
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

	// Build link map from existing pages BEFORE syncing
	// This allows links to resolve during initial sync (no second pass needed)
	fmt.Println("Building link map from existing pages...")
	s.debugf("Building link map for database: %s", s.config.RootPageID)
	linkMap, err := s.buildLinkMapForDatabase(s.config.RootPageID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: couldn't build link map, links won't resolve: %v\n", err)
		linkMap = make(map[string]string)
	}
	s.debugf("Link map built with %d entries", len(linkMap))

	// Create progress manager
	progress := NewProgressManager()
	progress.Start()
	defer progress.Stop()

	// Disable verbose output on the notion client since progress manager handles it
	s.notionClient.SetVerbose(false)

	// Sync all files with link resolution during parsing
	err = s.syncDirectoryRecursive(dirPath, s.config.RootPageID, dirPath, linkMap, progress)
	if err != nil {
		return err
	}

	return nil
}

// buildLinkMapForDatabase builds a map of markdown filenames to Notion page URLs
func (s *Syncer) buildLinkMapForDatabase(databaseID string) (map[string]string, error) {
	pageMap, err := s.notionClient.BuildFilenameToPageMap(databaseID)
	if err != nil {
		return nil, err
	}

	// Return page IDs directly (not URLs) for page mentions
	return pageMap, nil
}

// syncDirectoryRecursive recursively syncs a directory to Notion
// databaseID is the database to sync files into
func (s *Syncer) syncDirectoryRecursive(currentPath, databaseID, basePath string, linkMap map[string]string, progress *ProgressManager) error {
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

	// If this is not the base directory and has an index file, sync it
	if currentPath != basePath && indexFile != "" {
		dirName := filepath.Base(currentPath)
		fmt.Printf("Syncing directory '%s' using index file '%s'\n", dirName, filepath.Base(indexFile))
		// The index file content goes into the directory's database entry
		// which was created when we found/created the database
	}

	// Collect files and directories
	var mdFiles []string
	var subdirs []fs.DirEntry

	for _, entry := range entries {
		entryPath := filepath.Join(currentPath, entry.Name())

		if entry.IsDir() {
			subdirs = append(subdirs, entry)
		} else if strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			// Skip index files - they're used to populate directory entries
			if indexFile != "" && entryPath == indexFile {
				continue
			}
			mdFiles = append(mdFiles, entryPath)
		}
	}

	// Sync markdown files in parallel with progress
	if len(mdFiles) > 0 {
		fmt.Printf("Syncing %d files...\n", len(mdFiles))
		s.syncFilesParallel(mdFiles, databaseID, linkMap, progress)
	}

	// Process subdirectories sequentially (they create nested databases)
	for _, entry := range subdirs {
		entryPath := filepath.Join(currentPath, entry.Name())
		dirName := entry.Name()
		fmt.Printf("Processing subdirectory: %s\n", dirName)

		// Find or create a page entry for this directory, then get/create its child database
		childDatabaseID, err := s.findOrCreateSubdirectoryDatabase(databaseID, dirName, currentPath, entryPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create database for %s: %v\n", dirName, err)
			continue
		}

		// Build link map for child database (for links within subdirectory)
		childLinkMap, err := s.buildLinkMapForDatabase(childDatabaseID)
		if err != nil {
			childLinkMap = make(map[string]string)
		}
		// Merge parent link map for cross-directory links
		for k, v := range linkMap {
			if _, exists := childLinkMap[k]; !exists {
				childLinkMap[k] = v
			}
		}

		// Recursively sync the subdirectory
		err = s.syncDirectoryRecursive(entryPath, childDatabaseID, basePath, childLinkMap, progress)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to sync directory %s: %v\n", entryPath, err)
		}
	}

	return nil
}

// syncFilesParallel syncs files in parallel with interactive progress display
func (s *Syncer) syncFilesParallel(files []string, databaseID string, linkMap map[string]string, progress *ProgressManager) {
	concurrency := s.config.getConcurrency()
	s.debugf("Starting parallel sync of %d files with concurrency %d", len(files), concurrency)
	sem := make(chan struct{}, concurrency)
	var wg gosync.WaitGroup

	for i, filePath := range files {
		wg.Add(1)
		s.debugf("[%d/%d] Waiting for semaphore: %s", i+1, len(files), filepath.Base(filePath))
		sem <- struct{}{}
		s.debugf("[%d/%d] Acquired semaphore: %s", i+1, len(files), filepath.Base(filePath))

		go func(fp string, idx int) {
			defer wg.Done()
			defer func() {
				<-sem
				s.debugf("[%d/%d] Released semaphore: %s", idx+1, len(files), filepath.Base(fp))
			}()

			fileName := filepath.Base(fp)
			s.debugf("[%d/%d] Starting sync: %s", idx+1, len(files), fileName)

			_, created, skipped, err := s.syncFileWithProgress(fp, databaseID, "", linkMap, progress)

			if err != nil {
				s.debugf("[%d/%d] Error syncing %s: %v", idx+1, len(files), fileName, err)
				progress.PrintError(fileName, err)
			} else if skipped {
				s.debugf("[%d/%d] Skipped (unchanged): %s", idx+1, len(files), fileName)
				progress.PrintCompleted(fileName, "unchanged")
			} else if created {
				s.debugf("[%d/%d] Created: %s", idx+1, len(files), fileName)
				progress.PrintCompleted(fileName, "created")
			} else {
				s.debugf("[%d/%d] Updated: %s", idx+1, len(files), fileName)
				progress.PrintCompleted(fileName, "updated")
			}
		}(filePath, i)
	}

	s.debugf("Waiting for all goroutines to complete...")
	wg.Wait()
	s.debugf("All goroutines completed")
}

// syncFileWithProgress syncs a single file with progress callback support
func (s *Syncer) syncFileWithProgress(filePath, databaseID, customIdentifier string, linkMap map[string]string, progress *ProgressManager) (string, bool, bool, error) {
	fileName := filepath.Base(filePath)
	s.debugf("  [%s] Reading file", fileName)

	// Read the markdown file
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", false, false, fmt.Errorf("failed to read file: %w", err)
	}
	s.debugf("  [%s] Read %d bytes", fileName, len(content))

	// Compute content hash
	contentHash := notion.ComputeContentHash(content)
	s.debugf("  [%s] Content hash: %s", fileName, contentHash[:8])

	// Parse markdown to extract title and content
	s.debugf("  [%s] Parsing markdown", fileName)
	doc, err := markdown.Parse(content)
	if err != nil {
		return "", false, false, fmt.Errorf("failed to parse markdown: %w", err)
	}
	s.debugf("  [%s] Parsed: title=%q, blocks=%d", fileName, doc.Title, len(doc.Blocks))

	// Resolve relative links if we have a link map
	if linkMap != nil && len(linkMap) > 0 {
		markdown.ResolveRelativeLinks(doc.Blocks, linkMap)
	}

	// Determine the identifier (filename or custom)
	var identifier string
	if customIdentifier != "" {
		identifier = customIdentifier
	} else {
		identifier = filepath.Base(filePath)
	}

	// Look up if an entry exists with this identifier
	s.debugf("  [%s] Looking up existing entry in database", fileName)
	pageID, exists, err := s.notionClient.FindEntryByFilename(databaseID, identifier)
	if err != nil {
		return "", false, false, fmt.Errorf("failed to lookup entry: %w", err)
	}
	s.debugf("  [%s] Entry exists: %v, pageID: %s", fileName, exists, pageID)

	if exists {
		// Check if content has changed (unless force is set)
		if !s.config.Force {
			s.debugf("  [%s] Checking content hash", fileName)
			existingHash, err := s.notionClient.GetEntryContentHash(pageID)
			if err == nil && existingHash == contentHash {
				s.debugf("  [%s] Content unchanged, skipping", fileName)
				// Content unchanged, skip update
				return pageID, false, true, nil
			}
			s.debugf("  [%s] Content changed (old=%s, new=%s)", fileName, existingHash[:8], contentHash[:8])
		} else {
			s.debugf("  [%s] Force flag set, skipping hash check", fileName)
		}

		// Register file for progress tracking
		var callback notion.ProgressCallback
		if progress != nil {
			progress.AddActiveFile(fileName, len(doc.Blocks))
			callback = progress.ProgressCallback(fileName)
		}

		// Update existing entry with progress
		s.debugf("  [%s] Updating entry...", fileName)
		err = s.notionClient.UpdateDatabaseEntryWithProgress(pageID, doc.Title, identifier, contentHash, doc.Blocks, callback)
		if progress != nil {
			progress.RemoveActiveFile(fileName)
		}
		if err != nil {
			return "", false, false, fmt.Errorf("failed to update entry: %w", err)
		}
		s.debugf("  [%s] Update complete", fileName)
		return pageID, false, false, nil
	}

	// Create new entry with progress
	var callback notion.ProgressCallback
	if progress != nil {
		progress.AddActiveFile(fileName, len(doc.Blocks))
		callback = progress.ProgressCallback(fileName)
	}

	s.debugf("  [%s] Creating new entry...", fileName)
	pageID, err = s.notionClient.CreateDatabaseEntryWithProgress(databaseID, doc.Title, identifier, contentHash, doc.Blocks, callback)
	if progress != nil {
		progress.RemoveActiveFile(fileName)
	}
	if err != nil {
		return "", false, false, fmt.Errorf("failed to create entry: %w", err)
	}
	s.debugf("  [%s] Create complete, pageID: %s", fileName, pageID)

	return pageID, true, false, nil
}

// findOrCreateSubdirectoryDatabase finds or creates a database for a subdirectory
// It creates a page entry in the parent database, then creates a child database in that page
func (s *Syncer) findOrCreateSubdirectoryDatabase(parentDatabaseID, dirName, parentPath, dirPath string) (string, error) {
	// First, check if we already have a page entry for this directory
	pageID, exists, err := s.notionClient.FindEntryByFilename(parentDatabaseID, dirName)
	if err != nil {
		return "", fmt.Errorf("failed to find directory entry: %w", err)
	}

	// Check for index file to get content for the directory page
	var indexContent []notionapi.Block
	var indexTitle string
	var contentHash string
	indexFile := findIndexFile(dirPath)
	if indexFile != "" {
		content, err := os.ReadFile(indexFile)
		if err == nil {
			contentHash = notion.ComputeContentHash(content)
			doc, err := markdown.Parse(content)
			if err == nil {
				indexContent = doc.Blocks
				indexTitle = doc.Title
			}
		}
	}

	// Use directory name as title if no index file
	if indexTitle == "" {
		indexTitle = dirName
	}

	if !exists {
		// Create a page entry for the directory
		pageID, err = s.notionClient.CreateDatabaseEntry(parentDatabaseID, indexTitle, dirName, contentHash, indexContent)
		if err != nil {
			return "", fmt.Errorf("failed to create directory entry: %w", err)
		}
		fmt.Printf("  ✓ Created directory page: %s\n", dirName)
	} else {
		// Update the existing page with index content if available
		if len(indexContent) > 0 {
			// Check if content has changed
			existingHash, _ := s.notionClient.GetEntryContentHash(pageID)
			if existingHash != contentHash {
				err = s.notionClient.UpdateDatabaseEntry(pageID, indexTitle, dirName, contentHash, indexContent)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to update directory page: %v\n", err)
				}
			}
		}
	}

	// Now find or create the child database within this page
	childDBID, dbExists, err := s.notionClient.FindChildDatabase(pageID, dirName)
	if err != nil {
		return "", fmt.Errorf("failed to find child database: %w", err)
	}

	if !dbExists {
		childDBID, err = s.notionClient.CreateChildDatabase(pageID, dirName, dirName)
		if err != nil {
			return "", fmt.Errorf("failed to create child database: %w", err)
		}
		fmt.Printf("  ✓ Created child database: %s\n", dirName)
	}

	return childDBID, nil
}

// findIndexFile looks for index.md or README.md in a directory
func findIndexFile(dirPath string) string {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		lowerName := strings.ToLower(entry.Name())
		if lowerName == "index.md" || lowerName == "readme.md" {
			return filepath.Join(dirPath, entry.Name())
		}
	}
	return ""
}

// syncFile syncs a single file to a database
// Returns: pageID, wasCreated, wasSkipped, error
func (s *Syncer) syncFile(filePath, databaseID, customIdentifier string, linkMap map[string]string) (string, bool, bool, error) {
	// Read the markdown file
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", false, false, fmt.Errorf("failed to read file: %w", err)
	}

	// Compute content hash
	contentHash := notion.ComputeContentHash(content)

	// Parse markdown to extract title and content
	doc, err := markdown.Parse(content)
	if err != nil {
		return "", false, false, fmt.Errorf("failed to parse markdown: %w", err)
	}

	// Resolve relative links if we have a link map
	if linkMap != nil && len(linkMap) > 0 {
		markdown.ResolveRelativeLinks(doc.Blocks, linkMap)
	}

	// Determine the identifier (filename or custom)
	var identifier string
	if customIdentifier != "" {
		identifier = customIdentifier
	} else {
		identifier = filepath.Base(filePath)
	}

	// Look up if an entry exists with this identifier
	pageID, exists, err := s.notionClient.FindEntryByFilename(databaseID, identifier)
	if err != nil {
		return "", false, false, fmt.Errorf("failed to lookup entry: %w", err)
	}

	if exists {
		// Check if content has changed (unless force is set)
		if !s.config.Force {
			existingHash, err := s.notionClient.GetEntryContentHash(pageID)
			if err == nil && existingHash == contentHash {
				// Content unchanged, skip update
				return pageID, false, true, nil
			}
		}

		// Update existing entry
		err = s.notionClient.UpdateDatabaseEntry(pageID, doc.Title, identifier, contentHash, doc.Blocks)
		if err != nil {
			return "", false, false, fmt.Errorf("failed to update entry: %w", err)
		}
		return pageID, false, false, nil
	}

	// Create new entry
	pageID, err = s.notionClient.CreateDatabaseEntry(databaseID, doc.Title, identifier, contentHash, doc.Blocks)
	if err != nil {
		return "", false, false, fmt.Errorf("failed to create entry: %w", err)
	}

	return pageID, true, false, nil
}

// CleanupOrphanedPages deletes Notion entries that don't have corresponding markdown files
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

	// Get all entries from the root database
	entries, err := s.notionClient.GetDatabaseEntries(s.config.RootPageID)
	if err != nil {
		return fmt.Errorf("failed to get database entries: %w", err)
	}

	// Check each entry
	for _, entry := range entries {
		// Get the "Markdown File" property
		if prop, ok := entry.Properties["Markdown File"]; ok {
			if richTextProp, ok := prop.(*notionapi.RichTextProperty); ok {
				if len(richTextProp.RichText) > 0 {
					entryFilename := richTextProp.RichText[0].PlainText
					// If the markdown file or directory doesn't exist, delete the entry
					if !fileSet[entryFilename] {
						fmt.Printf("⚠ Deleting orphaned entry '%s' (no markdown file or directory: %s)\n", getEntryTitle(entry), entryFilename)
						err = s.notionClient.DeleteEntry(string(entry.ID))
						if err != nil {
							fmt.Fprintf(os.Stderr, "Warning: failed to delete entry %s: %v\n", entry.ID, err)
						}
					}
				}
			}
		}
	}

	return nil
}

// getEntryTitle extracts the title from a database entry
func getEntryTitle(entry notionapi.Page) string {
	// Try different common title property names
	titleProps := []string{"title", "Title", "Name", "name"}

	for _, propName := range titleProps {
		if prop, ok := entry.Properties[propName]; ok {
			if titleProp, ok := prop.(*notionapi.TitleProperty); ok {
				if len(titleProp.Title) > 0 {
					return titleProp.Title[0].PlainText
				}
			}
		}
	}

	return "Untitled"
}

