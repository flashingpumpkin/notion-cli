package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/flashingpumpkin/notion-cli/internal/markdown"
	"github.com/flashingpumpkin/notion-cli/internal/notion"
)

// Config holds the configuration for syncing
type Config struct {
	FilePath    string
	NotionToken string
	RootPageID  string
	DatabaseID  string
}

// Syncer handles syncing markdown files to Notion
type Syncer struct {
	config       Config
	notionClient *notion.Client
	mappingFile  string
}

// NewSyncer creates a new Syncer instance
func NewSyncer(config Config) (*Syncer, error) {
	client, err := notion.NewClient(config.NotionToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create notion client: %w", err)
	}

	// Create mapping file path in the same directory as the markdown file
	dir := filepath.Dir(config.FilePath)
	mappingFile := filepath.Join(dir, ".notion-sync-mappings.json")

	return &Syncer{
		config:       config,
		notionClient: client,
		mappingFile:  mappingFile,
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

	// Check if this file already has a mapping to a Notion page
	fileHash := s.getFileHash(s.config.FilePath)
	pageID, exists := s.getPageMapping(fileHash)

	if exists {
		// Update existing page
		err = s.notionClient.UpdatePage(pageID, doc.Title, doc.Blocks)
		if err != nil {
			return "", false, fmt.Errorf("failed to update page: %w", err)
		}
		return pageID, false, nil
	}

	// Create new page
	pageID, err = s.notionClient.CreatePage(s.config.RootPageID, doc.Title, doc.Blocks)
	if err != nil {
		return "", false, fmt.Errorf("failed to create page: %w", err)
	}

	// Save the mapping
	err = s.savePageMapping(fileHash, pageID)
	if err != nil {
		return "", false, fmt.Errorf("failed to save mapping: %w", err)
	}

	return pageID, true, nil
}

// getFileHash returns a hash for the file path to use as a key
func (s *Syncer) getFileHash(filePath string) string {
	absPath, _ := filepath.Abs(filePath)
	hash := sha256.Sum256([]byte(absPath))
	return hex.EncodeToString(hash[:])
}

// getPageMapping retrieves the Notion page ID for a given file hash
func (s *Syncer) getPageMapping(fileHash string) (string, bool) {
	mappings, err := s.loadMappings()
	if err != nil {
		return "", false
	}

	pageID, exists := mappings[fileHash]
	return pageID, exists
}

// savePageMapping saves the mapping between file hash and Notion page ID
func (s *Syncer) savePageMapping(fileHash, pageID string) error {
	mappings, _ := s.loadMappings()
	if mappings == nil {
		mappings = make(map[string]string)
	}

	mappings[fileHash] = pageID

	data, err := json.MarshalIndent(mappings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal mappings: %w", err)
	}

	err = os.WriteFile(s.mappingFile, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write mappings file: %w", err)
	}

	return nil
}

// loadMappings loads the mappings from file
func (s *Syncer) loadMappings() (map[string]string, error) {
	data, err := os.ReadFile(s.mappingFile)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, err
	}

	var mappings map[string]string
	err = json.Unmarshal(data, &mappings)
	if err != nil {
		return nil, err
	}

	return mappings, nil
}
