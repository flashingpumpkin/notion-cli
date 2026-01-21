package notion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"github.com/jomei/notionapi"
)

// Client wraps the Notion API client
type Client struct {
	api                   *notionapi.Client
	ensuredDatabases      map[string]bool
	ensuredDatabasesMutex sync.Mutex
	verbose               bool // When true, print detailed progress
}

// NewClient creates a new Notion client
func NewClient(token string) (*Client, error) {
	if token == "" {
		return nil, fmt.Errorf("notion token is required")
	}

	client := notionapi.NewClient(notionapi.Token(token))
	return &Client{
		api:              client,
		ensuredDatabases: make(map[string]bool),
		verbose:          true, // Default to verbose
	}, nil
}

// SetVerbose enables or disables verbose progress output
func (c *Client) SetVerbose(v bool) {
	c.verbose = v
}

// maxBlocksPerRequest is the Notion API limit for blocks per request
const maxBlocksPerRequest = 100

// markdownFileProperty is the property name used to track source files
const markdownFileProperty = "Markdown File"

// contentHashProperty is the property name used to track content changes
const contentHashProperty = "Content Hash"

// lookahead is how far to search for matches in the diff algorithm
const lookahead = 5

// OperationType represents the type of block operation
type OperationType int

const (
	OpUnchanged OperationType = iota
	OpUpdate
	OpDelete
	OpInsert
	OpAppend
)

// Operation represents a single block diff operation
type Operation struct {
	Type       OperationType
	OldIndex   int              // Index in existing blocks (for Update/Delete)
	NewIndex   int              // Index in new blocks (for Insert/Append/Update)
	BlockID    notionapi.BlockID // Block ID for Update/Delete operations
	AfterID    notionapi.BlockID // Block ID to insert after (for Insert)
	Blocks     []notionapi.Block // Blocks for Insert/Append operations
}

// DiffResult contains the computed diff and summary statistics
type DiffResult struct {
	Operations []Operation
	Unchanged  int
	Updated    int
	Deleted    int
	Inserted   int
	Appended   int
}

// EnsureMarkdownFileProperty ensures the database has required tracking properties
func (c *Client) EnsureMarkdownFileProperty(databaseID string) error {
	// Check cache first (with lock for thread safety)
	c.ensuredDatabasesMutex.Lock()
	if c.ensuredDatabases[databaseID] {
		c.ensuredDatabasesMutex.Unlock()
		return nil
	}
	c.ensuredDatabasesMutex.Unlock()

	ctx := context.Background()

	// Get the database to check existing properties
	db, err := c.api.Database.Get(ctx, notionapi.DatabaseID(databaseID))
	if err != nil {
		return fmt.Errorf("failed to get database: %w", err)
	}

	// Check which properties need to be added
	propsToAdd := notionapi.PropertyConfigs{}

	if _, exists := db.Properties[markdownFileProperty]; !exists {
		propsToAdd[markdownFileProperty] = notionapi.RichTextPropertyConfig{
			Type: notionapi.PropertyConfigTypeRichText,
		}
	}

	if _, exists := db.Properties[contentHashProperty]; !exists {
		propsToAdd[contentHashProperty] = notionapi.RichTextPropertyConfig{
			Type: notionapi.PropertyConfigTypeRichText,
		}
	}

	// Mark as ensured before making the API call to prevent races
	c.ensuredDatabasesMutex.Lock()
	c.ensuredDatabases[databaseID] = true
	c.ensuredDatabasesMutex.Unlock()

	if len(propsToAdd) == 0 {
		return nil
	}

	// Add the properties to the database
	updateRequest := &notionapi.DatabaseUpdateRequest{
		Properties: propsToAdd,
	}

	_, err = c.api.Database.Update(ctx, notionapi.DatabaseID(databaseID), updateRequest)
	if err != nil {
		return fmt.Errorf("failed to add properties: %w", err)
	}

	return nil
}

// ComputeContentHash computes a SHA256 hash of the content
func ComputeContentHash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

// GetEntryContentHash retrieves the content hash from a database entry
func (c *Client) GetEntryContentHash(pageID string) (string, error) {
	ctx := context.Background()

	page, err := c.api.Page.Get(ctx, notionapi.PageID(pageID))
	if err != nil {
		return "", err
	}

	if prop, ok := page.Properties[contentHashProperty]; ok {
		if richTextProp, ok := prop.(*notionapi.RichTextProperty); ok {
			if len(richTextProp.RichText) > 0 {
				return richTextProp.RichText[0].PlainText, nil
			}
		}
	}

	return "", nil
}

// ProgressCallback is called with current and total block counts during sync
type ProgressCallback func(current, total int)

// CreateDatabaseEntry creates a new page within a database
func (c *Client) CreateDatabaseEntry(databaseID, title, filename, contentHash string, blocks []notionapi.Block) (string, error) {
	ctx := context.Background()

	// Ensure the property exists
	if err := c.EnsureMarkdownFileProperty(databaseID); err != nil {
		return "", err
	}

	// Split blocks: first batch goes with page creation, rest are appended after
	var initialBlocks []notionapi.Block
	var remainingBlocks []notionapi.Block
	if len(blocks) > maxBlocksPerRequest {
		initialBlocks = blocks[:maxBlocksPerRequest]
		remainingBlocks = blocks[maxBlocksPerRequest:]
	} else {
		initialBlocks = blocks
	}

	// Create the page request within the database
	request := &notionapi.PageCreateRequest{
		Parent: notionapi.Parent{
			Type:       notionapi.ParentTypeDatabaseID,
			DatabaseID: notionapi.DatabaseID(databaseID),
		},
		Properties: notionapi.Properties{
			"title": notionapi.TitleProperty{
				Type: notionapi.PropertyTypeTitle,
				Title: []notionapi.RichText{
					{
						Type: notionapi.ObjectTypeText,
						Text: &notionapi.Text{
							Content: title,
						},
					},
				},
			},
			markdownFileProperty: notionapi.RichTextProperty{
				Type: notionapi.PropertyTypeRichText,
				RichText: []notionapi.RichText{
					{
						Type: notionapi.ObjectTypeText,
						Text: &notionapi.Text{
							Content: filename,
						},
					},
				},
			},
			contentHashProperty: notionapi.RichTextProperty{
				Type: notionapi.PropertyTypeRichText,
				RichText: []notionapi.RichText{
					{
						Type: notionapi.ObjectTypeText,
						Text: &notionapi.Text{
							Content: contentHash,
						},
					},
				},
			},
		},
		Children: initialBlocks,
	}

	page, err := c.api.Page.Create(ctx, request)
	if err != nil {
		return "", fmt.Errorf("failed to create database entry: %w", err)
	}

	pageID := string(page.ID)

	// Append remaining blocks in batches
	if err := c.appendBlocksInBatches(ctx, notionapi.BlockID(pageID), remainingBlocks); err != nil {
		return pageID, fmt.Errorf("entry created but failed to append all blocks: %w", err)
	}

	return pageID, nil
}

// CreateDatabaseEntryWithProgress creates a new page within a database with progress callback
func (c *Client) CreateDatabaseEntryWithProgress(databaseID, title, filename, contentHash string, blocks []notionapi.Block, onProgress ProgressCallback) (string, error) {
	ctx := context.Background()

	// Ensure the property exists
	if err := c.EnsureMarkdownFileProperty(databaseID); err != nil {
		return "", err
	}

	// Split blocks: first batch goes with page creation, rest are appended after
	var initialBlocks []notionapi.Block
	var remainingBlocks []notionapi.Block
	if len(blocks) > maxBlocksPerRequest {
		initialBlocks = blocks[:maxBlocksPerRequest]
		remainingBlocks = blocks[maxBlocksPerRequest:]
	} else {
		initialBlocks = blocks
	}

	// Create the page request within the database
	request := &notionapi.PageCreateRequest{
		Parent: notionapi.Parent{
			Type:       notionapi.ParentTypeDatabaseID,
			DatabaseID: notionapi.DatabaseID(databaseID),
		},
		Properties: notionapi.Properties{
			"title": notionapi.TitleProperty{
				Type: notionapi.PropertyTypeTitle,
				Title: []notionapi.RichText{
					{
						Type: notionapi.ObjectTypeText,
						Text: &notionapi.Text{
							Content: title,
						},
					},
				},
			},
			markdownFileProperty: notionapi.RichTextProperty{
				Type: notionapi.PropertyTypeRichText,
				RichText: []notionapi.RichText{
					{
						Type: notionapi.ObjectTypeText,
						Text: &notionapi.Text{
							Content: filename,
						},
					},
				},
			},
			contentHashProperty: notionapi.RichTextProperty{
				Type: notionapi.PropertyTypeRichText,
				RichText: []notionapi.RichText{
					{
						Type: notionapi.ObjectTypeText,
						Text: &notionapi.Text{
							Content: contentHash,
						},
					},
				},
			},
		},
		Children: initialBlocks,
	}

	page, err := c.api.Page.Create(ctx, request)
	if err != nil {
		return "", fmt.Errorf("failed to create database entry: %w", err)
	}

	pageID := string(page.ID)

	// Report initial progress
	if onProgress != nil {
		onProgress(len(initialBlocks), len(blocks))
	}

	// Append remaining blocks in batches with progress
	if err := c.appendBlocksInBatchesWithProgress(ctx, notionapi.BlockID(pageID), remainingBlocks, len(initialBlocks), len(blocks), onProgress); err != nil {
		return pageID, fmt.Errorf("entry created but failed to append all blocks: %w", err)
	}

	return pageID, nil
}

// UpdateDatabaseEntry updates an existing database entry
func (c *Client) UpdateDatabaseEntry(pageID, title, filename, contentHash string, blocks []notionapi.Block) error {
	ctx := context.Background()

	// Update the page title, filename, and content hash
	updateRequest := &notionapi.PageUpdateRequest{
		Properties: notionapi.Properties{
			"title": notionapi.TitleProperty{
				Type: notionapi.PropertyTypeTitle,
				Title: []notionapi.RichText{
					{
						Type: notionapi.ObjectTypeText,
						Text: &notionapi.Text{
							Content: title,
						},
					},
				},
			},
			markdownFileProperty: notionapi.RichTextProperty{
				Type: notionapi.PropertyTypeRichText,
				RichText: []notionapi.RichText{
					{
						Type: notionapi.ObjectTypeText,
						Text: &notionapi.Text{
							Content: filename,
						},
					},
				},
			},
			contentHashProperty: notionapi.RichTextProperty{
				Type: notionapi.PropertyTypeRichText,
				RichText: []notionapi.RichText{
					{
						Type: notionapi.ObjectTypeText,
						Text: &notionapi.Text{
							Content: contentHash,
						},
					},
				},
			},
		},
	}

	_, err := c.api.Page.Update(ctx, notionapi.PageID(pageID), updateRequest)
	if err != nil {
		return fmt.Errorf("failed to update entry: %w", err)
	}

	// Get all existing blocks with pagination
	existingBlocks, err := c.getAllChildBlocks(ctx, notionapi.BlockID(pageID))
	if err != nil {
		return fmt.Errorf("failed to get existing blocks: %w", err)
	}

	// Smart update: try to update in place where possible
	return c.smartUpdateBlocks(ctx, notionapi.BlockID(pageID), existingBlocks, blocks)
}

// UpdateDatabaseEntryWithProgress updates an existing database entry with progress callback
func (c *Client) UpdateDatabaseEntryWithProgress(pageID, title, filename, contentHash string, blocks []notionapi.Block, onProgress ProgressCallback) error {
	ctx := context.Background()

	// Update the page title, filename, and content hash
	updateRequest := &notionapi.PageUpdateRequest{
		Properties: notionapi.Properties{
			"title": notionapi.TitleProperty{
				Type: notionapi.PropertyTypeTitle,
				Title: []notionapi.RichText{
					{
						Type: notionapi.ObjectTypeText,
						Text: &notionapi.Text{
							Content: title,
						},
					},
				},
			},
			markdownFileProperty: notionapi.RichTextProperty{
				Type: notionapi.PropertyTypeRichText,
				RichText: []notionapi.RichText{
					{
						Type: notionapi.ObjectTypeText,
						Text: &notionapi.Text{
							Content: filename,
						},
					},
				},
			},
			contentHashProperty: notionapi.RichTextProperty{
				Type: notionapi.PropertyTypeRichText,
				RichText: []notionapi.RichText{
					{
						Type: notionapi.ObjectTypeText,
						Text: &notionapi.Text{
							Content: contentHash,
						},
					},
				},
			},
		},
	}

	_, err := c.api.Page.Update(ctx, notionapi.PageID(pageID), updateRequest)
	if err != nil {
		return fmt.Errorf("failed to update entry: %w", err)
	}

	// Get all existing blocks with pagination
	existingBlocks, err := c.getAllChildBlocks(ctx, notionapi.BlockID(pageID))
	if err != nil {
		return fmt.Errorf("failed to get existing blocks: %w", err)
	}

	// Smart update with progress callback
	return c.smartUpdateBlocksWithProgress(ctx, notionapi.BlockID(pageID), existingBlocks, blocks, onProgress)
}

// getAllChildBlocks fetches all child blocks of a page/block with pagination
func (c *Client) getAllChildBlocks(ctx context.Context, blockID notionapi.BlockID) ([]notionapi.Block, error) {
	var allBlocks []notionapi.Block
	var cursor notionapi.Cursor

	for {
		pagination := &notionapi.Pagination{
			StartCursor: cursor,
			PageSize:    100,
		}

		resp, err := c.api.Block.GetChildren(ctx, blockID, pagination)
		if err != nil {
			return nil, err
		}

		allBlocks = append(allBlocks, resp.Results...)

		if !resp.HasMore {
			break
		}
		cursor = notionapi.Cursor(resp.NextCursor)
	}

	return allBlocks, nil
}

// SyncToPage syncs blocks directly to a page (not a database entry)
// This is used when syncing a single markdown file to an existing page
func (c *Client) SyncToPage(pageID string, blocks []notionapi.Block) error {
	ctx := context.Background()

	// Get all existing blocks with pagination
	existingBlocks, err := c.getAllChildBlocks(ctx, notionapi.BlockID(pageID))
	if err != nil {
		return fmt.Errorf("failed to get existing blocks: %w", err)
	}

	// Smart update: try to update in place where possible
	return c.smartUpdateBlocks(ctx, notionapi.BlockID(pageID), existingBlocks, blocks)
}

// IsPage checks if the given ID is a page (returns true) or database (returns false)
func (c *Client) IsPage(id string) (bool, error) {
	ctx := context.Background()

	// Try to get it as a page first
	_, err := c.api.Page.Get(ctx, notionapi.PageID(id))
	if err == nil {
		return true, nil
	}

	// Try as a database
	_, err = c.api.Database.Get(ctx, notionapi.DatabaseID(id))
	if err == nil {
		return false, nil
	}

	return false, fmt.Errorf("ID is neither a valid page nor database: %s", id)
}

// smartUpdateBlocks uses diff algorithm to minimise API calls
func (c *Client) smartUpdateBlocks(ctx context.Context, parentID notionapi.BlockID, existing []notionapi.Block, newBlocks []notionapi.Block) error {
	// Filter out child databases from existing blocks (we don't want to touch those)
	var filteredExisting []notionapi.Block
	for _, b := range existing {
		if b.GetType() != notionapi.BlockTypeChildDatabase {
			filteredExisting = append(filteredExisting, b)
		}
	}

	// Compute the diff
	diff := computeBlockDiff(filteredExisting, newBlocks)

	// If everything is unchanged, we're done
	if diff.Unchanged == len(filteredExisting) && diff.Unchanged == len(newBlocks) {
		if c.verbose {
			fmt.Printf("  %d blocks unchanged\n", diff.Unchanged)
		}
		return nil
	}

	// Collect blocks to delete and blocks to append
	var toDelete []notionapi.Block
	var toAppend []notionapi.Block
	var toUpdate []Operation

	for _, op := range diff.Operations {
		switch op.Type {
		case OpDelete:
			toDelete = append(toDelete, filteredExisting[op.OldIndex])
		case OpAppend:
			toAppend = append(toAppend, op.Blocks...)
		case OpUpdate:
			toUpdate = append(toUpdate, op)
		}
	}

	// Execute updates in parallel
	if len(toUpdate) > 0 {
		if c.verbose {
			fmt.Printf("  Updating %d blocks...\n", len(toUpdate))
		}
		c.updateBlocksParallel(ctx, toUpdate)
	}

	// Delete blocks in parallel
	if len(toDelete) > 0 {
		if c.verbose {
			fmt.Printf("  Deleting %d blocks...\n", len(toDelete))
		}
		c.deleteBlocksParallel(ctx, toDelete)
	}

	// Append new blocks
	if len(toAppend) > 0 {
		if c.verbose {
			fmt.Printf("  Appending %d blocks...\n", len(toAppend))
		}
		if err := c.appendBlocksInBatches(ctx, parentID, toAppend); err != nil {
			return fmt.Errorf("failed to append blocks: %w", err)
		}
	}

	if c.verbose {
		fmt.Printf("  Blocks: %d unchanged, %d updated, %d deleted, %d appended\n",
			diff.Unchanged, len(toUpdate), len(toDelete), len(toAppend))
	}

	return nil
}

// smartUpdateBlocksWithProgress uses diff algorithm to minimise API calls with progress reporting
func (c *Client) smartUpdateBlocksWithProgress(ctx context.Context, parentID notionapi.BlockID, existing []notionapi.Block, newBlocks []notionapi.Block, onProgress ProgressCallback) error {
	// Filter out child databases from existing blocks (we don't want to touch those)
	var filteredExisting []notionapi.Block
	for _, b := range existing {
		if b.GetType() != notionapi.BlockTypeChildDatabase {
			filteredExisting = append(filteredExisting, b)
		}
	}

	// Compute the diff
	diff := computeBlockDiff(filteredExisting, newBlocks)

	// If everything is unchanged, we're done
	if diff.Unchanged == len(filteredExisting) && diff.Unchanged == len(newBlocks) {
		if onProgress != nil {
			onProgress(len(newBlocks), len(newBlocks))
		}
		return nil
	}

	// Collect blocks to delete and blocks to append
	var toDelete []notionapi.Block
	var toAppend []notionapi.Block
	var toUpdate []Operation

	for _, op := range diff.Operations {
		switch op.Type {
		case OpDelete:
			toDelete = append(toDelete, filteredExisting[op.OldIndex])
		case OpAppend:
			toAppend = append(toAppend, op.Blocks...)
		case OpUpdate:
			toUpdate = append(toUpdate, op)
		}
	}

	// Track progress across all operations
	totalOps := len(toUpdate) + len(toDelete) + len(toAppend)
	completedOps := 0
	var progressMu sync.Mutex

	reportProgress := func(delta int) {
		if onProgress == nil {
			return
		}
		progressMu.Lock()
		completedOps += delta
		onProgress(completedOps, totalOps)
		progressMu.Unlock()
	}

	// Execute updates in parallel with progress
	if len(toUpdate) > 0 {
		c.updateBlocksParallelWithProgress(ctx, toUpdate, reportProgress)
	}

	// Delete blocks in parallel with progress
	if len(toDelete) > 0 {
		c.deleteBlocksParallelWithProgress(ctx, toDelete, reportProgress)
	}

	// Append new blocks with progress
	if len(toAppend) > 0 {
		if err := c.appendBlocksInBatchesWithProgress(ctx, parentID, toAppend, completedOps, totalOps, onProgress); err != nil {
			return fmt.Errorf("failed to append blocks: %w", err)
		}
	}

	return nil
}

// blocksAreEqual compares two blocks for equality (same type and content)
func blocksAreEqual(existing, new notionapi.Block) bool {
	if existing.GetType() != new.GetType() {
		return false
	}

	// Compare content based on block type
	switch e := existing.(type) {
	case *notionapi.ParagraphBlock:
		if n, ok := new.(*notionapi.ParagraphBlock); ok {
			return richTextEqual(e.Paragraph.RichText, n.Paragraph.RichText)
		}
	case *notionapi.Heading1Block:
		if n, ok := new.(*notionapi.Heading1Block); ok {
			return richTextEqual(e.Heading1.RichText, n.Heading1.RichText)
		}
	case *notionapi.Heading2Block:
		if n, ok := new.(*notionapi.Heading2Block); ok {
			return richTextEqual(e.Heading2.RichText, n.Heading2.RichText)
		}
	case *notionapi.Heading3Block:
		if n, ok := new.(*notionapi.Heading3Block); ok {
			return richTextEqual(e.Heading3.RichText, n.Heading3.RichText)
		}
	case *notionapi.BulletedListItemBlock:
		if n, ok := new.(*notionapi.BulletedListItemBlock); ok {
			return richTextEqual(e.BulletedListItem.RichText, n.BulletedListItem.RichText)
		}
	case *notionapi.NumberedListItemBlock:
		if n, ok := new.(*notionapi.NumberedListItemBlock); ok {
			return richTextEqual(e.NumberedListItem.RichText, n.NumberedListItem.RichText)
		}
	case *notionapi.ToDoBlock:
		if n, ok := new.(*notionapi.ToDoBlock); ok {
			return e.ToDo.Checked == n.ToDo.Checked && richTextEqual(e.ToDo.RichText, n.ToDo.RichText)
		}
	case *notionapi.QuoteBlock:
		if n, ok := new.(*notionapi.QuoteBlock); ok {
			return richTextEqual(e.Quote.RichText, n.Quote.RichText)
		}
	case *notionapi.CodeBlock:
		if n, ok := new.(*notionapi.CodeBlock); ok {
			return e.Code.Language == n.Code.Language && richTextEqual(e.Code.RichText, n.Code.RichText)
		}
	case *notionapi.DividerBlock:
		_, ok := new.(*notionapi.DividerBlock)
		return ok
	case *notionapi.TableBlock:
		// Tables are complex - always consider them different to be safe
		return false
	case *notionapi.ImageBlock:
		if n, ok := new.(*notionapi.ImageBlock); ok {
			return e.Image.GetURL() == n.Image.GetURL()
		}
	}

	// Unknown block type - consider different to be safe
	return false
}

// findBlockInRange searches for a block matching needle in haystack[start:end]
// Returns the offset from start if found, -1 if not found
func findBlockInRange(needle notionapi.Block, haystack []notionapi.Block, start, end int) int {
	if start < 0 {
		start = 0
	}
	if end > len(haystack) {
		end = len(haystack)
	}
	for i := start; i < end; i++ {
		if blocksAreEqual(needle, haystack[i]) {
			return i - start
		}
	}
	return -1
}

// computeBlockDiff computes the operations needed to transform existing blocks into new blocks
// This is a pure function that returns the operations without executing them
func computeBlockDiff(existing []notionapi.Block, newBlocks []notionapi.Block) DiffResult {
	result := DiffResult{}

	oldIdx := 0
	newIdx := 0
	var lastProcessedBlockID notionapi.BlockID

	for oldIdx < len(existing) || newIdx < len(newBlocks) {
		// Case 1: Old list exhausted - append remaining new blocks
		if oldIdx >= len(existing) {
			blocksToAppend := make([]notionapi.Block, len(newBlocks)-newIdx)
			copy(blocksToAppend, newBlocks[newIdx:])
			result.Operations = append(result.Operations, Operation{
				Type:    OpAppend,
				Blocks:  blocksToAppend,
				AfterID: lastProcessedBlockID,
			})
			result.Appended += len(blocksToAppend)
			break
		}

		// Case 2: New list exhausted - delete remaining old blocks
		if newIdx >= len(newBlocks) {
			for i := oldIdx; i < len(existing); i++ {
				result.Operations = append(result.Operations, Operation{
					Type:     OpDelete,
					OldIndex: i,
					BlockID:  existing[i].GetID(),
				})
				result.Deleted++
			}
			break
		}

		oldBlock := existing[oldIdx]
		newBlock := newBlocks[newIdx]

		// Case 3: Blocks identical - skip
		if blocksAreEqual(oldBlock, newBlock) {
			result.Operations = append(result.Operations, Operation{
				Type:     OpUnchanged,
				OldIndex: oldIdx,
				NewIndex: newIdx,
				BlockID:  oldBlock.GetID(),
			})
			result.Unchanged++
			lastProcessedBlockID = oldBlock.GetID()
			oldIdx++
			newIdx++
			continue
		}

		// Case 4: Blocks differ - use lookahead to determine action
		// Look for oldBlock in upcoming new blocks (indicates insertion needed)
		foundInNew := findBlockInRange(oldBlock, newBlocks, newIdx+1, newIdx+1+lookahead)
		// Look for newBlock in upcoming old blocks (indicates deletion needed)
		foundInOld := findBlockInRange(newBlock, existing, oldIdx+1, oldIdx+1+lookahead)

		if foundInNew >= 0 && (foundInOld < 0 || foundInNew <= foundInOld) {
			// Old block exists later in new list -> need to insert new blocks
			// Fall back to delete-remaining and append-remaining for reliable ordering
			for i := oldIdx; i < len(existing); i++ {
				result.Operations = append(result.Operations, Operation{
					Type:     OpDelete,
					OldIndex: i,
					BlockID:  existing[i].GetID(),
				})
				result.Deleted++
			}
			blocksToAppend := make([]notionapi.Block, len(newBlocks)-newIdx)
			copy(blocksToAppend, newBlocks[newIdx:])
			result.Operations = append(result.Operations, Operation{
				Type:   OpAppend,
				Blocks: blocksToAppend,
			})
			result.Appended += len(blocksToAppend)
			break

		} else if foundInOld >= 0 {
			// New block exists later in old list -> delete old blocks until we reach it
			numToDelete := foundInOld + 1 // +1 because foundInOld is offset from oldIdx+1
			for i := 0; i < numToDelete; i++ {
				result.Operations = append(result.Operations, Operation{
					Type:     OpDelete,
					OldIndex: oldIdx + i,
					BlockID:  existing[oldIdx+i].GetID(),
				})
				result.Deleted++
			}
			oldIdx += numToDelete
			// Don't advance newIdx - we'll match it on next iteration

		} else {
			// Neither found in lookahead - direct replacement
			if oldBlock.GetType() == newBlock.GetType() {
				// Same type - update in place
				result.Operations = append(result.Operations, Operation{
					Type:     OpUpdate,
					OldIndex: oldIdx,
					NewIndex: newIdx,
					BlockID:  oldBlock.GetID(),
					Blocks:   []notionapi.Block{newBlock},
				})
				result.Updated++
				lastProcessedBlockID = oldBlock.GetID()
				oldIdx++
				newIdx++
			} else {
				// Different type - fall back to delete-remaining and append-remaining
				for i := oldIdx; i < len(existing); i++ {
					result.Operations = append(result.Operations, Operation{
						Type:     OpDelete,
						OldIndex: i,
						BlockID:  existing[i].GetID(),
					})
					result.Deleted++
				}
				blocksToAppend := make([]notionapi.Block, len(newBlocks)-newIdx)
				copy(blocksToAppend, newBlocks[newIdx:])
				result.Operations = append(result.Operations, Operation{
					Type:   OpAppend,
					Blocks: blocksToAppend,
				})
				result.Appended += len(blocksToAppend)
				break
			}
		}
	}

	return result
}

// richTextEqual compares two RichText slices for equality
func richTextEqual(a, b []notionapi.RichText) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		// Get text content (prefer Text.Content, fall back to PlainText)
		aContent := a[i].PlainText
		bContent := b[i].PlainText
		if a[i].Text != nil && a[i].Text.Content != "" {
			aContent = a[i].Text.Content
		}
		if b[i].Text != nil && b[i].Text.Content != "" {
			bContent = b[i].Text.Content
		}
		if aContent != bContent {
			return false
		}

		// Compare links
		aHasLink := a[i].Text != nil && a[i].Text.Link != nil
		bHasLink := b[i].Text != nil && b[i].Text.Link != nil
		if aHasLink != bHasLink {
			return false
		}
		if aHasLink && bHasLink && a[i].Text.Link.Url != b[i].Text.Link.Url {
			return false
		}

		// Compare annotations (bold, italic, etc.)
		if a[i].Annotations != nil && b[i].Annotations != nil {
			if a[i].Annotations.Bold != b[i].Annotations.Bold ||
				a[i].Annotations.Italic != b[i].Annotations.Italic ||
				a[i].Annotations.Strikethrough != b[i].Annotations.Strikethrough ||
				a[i].Annotations.Code != b[i].Annotations.Code {
				return false
			}
		}
	}
	return true
}

// updateBlockInPlace updates a single block's content without deleting it
func (c *Client) updateBlockInPlace(ctx context.Context, blockID notionapi.BlockID, newBlock notionapi.Block) error {
	var updateReq *notionapi.BlockUpdateRequest

	switch b := newBlock.(type) {
	case *notionapi.ParagraphBlock:
		updateReq = &notionapi.BlockUpdateRequest{
			Paragraph: &notionapi.Paragraph{RichText: b.Paragraph.RichText},
		}
	case *notionapi.Heading1Block:
		updateReq = &notionapi.BlockUpdateRequest{
			Heading1: &notionapi.Heading{RichText: b.Heading1.RichText},
		}
	case *notionapi.Heading2Block:
		updateReq = &notionapi.BlockUpdateRequest{
			Heading2: &notionapi.Heading{RichText: b.Heading2.RichText},
		}
	case *notionapi.Heading3Block:
		updateReq = &notionapi.BlockUpdateRequest{
			Heading3: &notionapi.Heading{RichText: b.Heading3.RichText},
		}
	case *notionapi.BulletedListItemBlock:
		updateReq = &notionapi.BlockUpdateRequest{
			BulletedListItem: &notionapi.ListItem{RichText: b.BulletedListItem.RichText},
		}
	case *notionapi.NumberedListItemBlock:
		updateReq = &notionapi.BlockUpdateRequest{
			NumberedListItem: &notionapi.ListItem{RichText: b.NumberedListItem.RichText},
		}
	case *notionapi.ToDoBlock:
		updateReq = &notionapi.BlockUpdateRequest{
			ToDo: &notionapi.ToDo{RichText: b.ToDo.RichText, Checked: b.ToDo.Checked},
		}
	case *notionapi.QuoteBlock:
		updateReq = &notionapi.BlockUpdateRequest{
			Quote: &notionapi.Quote{RichText: b.Quote.RichText},
		}
	case *notionapi.CodeBlock:
		updateReq = &notionapi.BlockUpdateRequest{
			Code: &notionapi.Code{RichText: b.Code.RichText, Language: b.Code.Language},
		}
	default:
		// For unsupported block types, return error to trigger fallback
		return fmt.Errorf("unsupported block type for in-place update: %s", newBlock.GetType())
	}

	_, err := c.api.Block.Update(ctx, blockID, updateReq)
	return err
}

// updateBlocksParallel updates blocks in parallel with limited concurrency
func (c *Client) updateBlocksParallel(ctx context.Context, operations []Operation) {
	const maxConcurrent = 20
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	var mu sync.Mutex
	completed := 0
	total := len(operations)

	for _, op := range operations {
		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore

		go func(o Operation) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			if len(o.Blocks) > 0 {
				_ = c.updateBlockInPlace(ctx, o.BlockID, o.Blocks[0])
			}

			if c.verbose {
				mu.Lock()
				completed++
				fmt.Printf("    [%d/%d] updated\n", completed, total)
				mu.Unlock()
			}
		}(op)
	}

	wg.Wait()
}

// deleteBlocksParallel deletes blocks in parallel with limited concurrency
func (c *Client) deleteBlocksParallel(ctx context.Context, blocks []notionapi.Block) {
	const maxConcurrent = 20
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	var mu sync.Mutex
	completed := 0
	total := len(blocks)

	for _, block := range blocks {
		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore

		go func(b notionapi.Block) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			_, _ = c.api.Block.Delete(ctx, b.GetID())

			if c.verbose {
				mu.Lock()
				completed++
				fmt.Printf("    [%d/%d] deleted\n", completed, total)
				mu.Unlock()
			}
		}(block)
	}

	wg.Wait()
}

// appendBlocksInBatches appends blocks to a parent in batches of maxBlocksPerRequest
func (c *Client) appendBlocksInBatches(ctx context.Context, parentID notionapi.BlockID, blocks []notionapi.Block) error {
	if len(blocks) == 0 {
		return nil
	}

	total := len(blocks)
	for i := 0; i < len(blocks); i += maxBlocksPerRequest {
		end := i + maxBlocksPerRequest
		if end > len(blocks) {
			end = len(blocks)
		}
		batch := blocks[i:end]

		appendRequest := &notionapi.AppendBlockChildrenRequest{
			Children: batch,
		}
		_, err := c.api.Block.AppendChildren(ctx, parentID, appendRequest)
		if err != nil {
			return err
		}

		if c.verbose {
			fmt.Printf("    [%d/%d] appended\n", end, total)
		}
	}
	return nil
}

// updateBlocksParallelWithProgress updates blocks in parallel with progress callback
func (c *Client) updateBlocksParallelWithProgress(ctx context.Context, operations []Operation, reportProgress func(int)) {
	const maxConcurrent = 20
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	for _, op := range operations {
		wg.Add(1)
		sem <- struct{}{}

		go func(o Operation) {
			defer wg.Done()
			defer func() { <-sem }()

			if len(o.Blocks) > 0 {
				_ = c.updateBlockInPlace(ctx, o.BlockID, o.Blocks[0])
			}

			reportProgress(1)
		}(op)
	}

	wg.Wait()
}

// deleteBlocksParallelWithProgress deletes blocks in parallel with progress callback
func (c *Client) deleteBlocksParallelWithProgress(ctx context.Context, blocks []notionapi.Block, reportProgress func(int)) {
	const maxConcurrent = 20
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	for _, block := range blocks {
		wg.Add(1)
		sem <- struct{}{}

		go func(b notionapi.Block) {
			defer wg.Done()
			defer func() { <-sem }()

			_, _ = c.api.Block.Delete(ctx, b.GetID())

			reportProgress(1)
		}(block)
	}

	wg.Wait()
}

// appendBlocksInBatchesWithProgress appends blocks with progress callback
func (c *Client) appendBlocksInBatchesWithProgress(ctx context.Context, parentID notionapi.BlockID, blocks []notionapi.Block, startProgress, totalProgress int, onProgress ProgressCallback) error {
	if len(blocks) == 0 {
		return nil
	}

	completed := startProgress
	for i := 0; i < len(blocks); i += maxBlocksPerRequest {
		end := i + maxBlocksPerRequest
		if end > len(blocks) {
			end = len(blocks)
		}
		batch := blocks[i:end]

		appendRequest := &notionapi.AppendBlockChildrenRequest{
			Children: batch,
		}
		_, err := c.api.Block.AppendChildren(ctx, parentID, appendRequest)
		if err != nil {
			return err
		}

		completed += len(batch)
		if onProgress != nil {
			onProgress(completed, totalProgress)
		}
	}
	return nil
}

// FindEntryByFilename finds a database entry by its markdown filename property
func (c *Client) FindEntryByFilename(databaseID, filename string) (string, bool, error) {
	ctx := context.Background()

	// Ensure the property exists before querying
	if err := c.EnsureMarkdownFileProperty(databaseID); err != nil {
		return "", false, err
	}

	// Query the database for entries with matching filename
	query := &notionapi.DatabaseQueryRequest{
		Filter: notionapi.PropertyFilter{
			Property: markdownFileProperty,
			RichText: &notionapi.TextFilterCondition{
				Equals: filename,
			},
		},
	}

	result, err := c.api.Database.Query(ctx, notionapi.DatabaseID(databaseID), query)
	if err != nil {
		return "", false, fmt.Errorf("failed to query database: %w", err)
	}

	if len(result.Results) > 0 {
		return string(result.Results[0].ID), true, nil
	}

	return "", false, nil
}

// CreateChildDatabase creates an inline database within a parent page
func (c *Client) CreateChildDatabase(parentPageID, title, identifier string) (string, error) {
	ctx := context.Background()

	// Add whitespace before the inline database
	emptyParagraph := &notionapi.AppendBlockChildrenRequest{
		Children: []notionapi.Block{
			&notionapi.ParagraphBlock{
				BasicBlock: notionapi.BasicBlock{
					Object: notionapi.ObjectTypeBlock,
					Type:   notionapi.BlockTypeParagraph,
				},
				Paragraph: notionapi.Paragraph{
					RichText: []notionapi.RichText{},
				},
			},
		},
	}
	_, _ = c.api.Block.AppendChildren(ctx, notionapi.BlockID(parentPageID), emptyParagraph)

	// Create a new inline database as a child of the page
	request := &notionapi.DatabaseCreateRequest{
		Parent: notionapi.Parent{
			Type:   notionapi.ParentTypePageID,
			PageID: notionapi.PageID(parentPageID),
		},
		IsInline: true,
		Title: []notionapi.RichText{
			{
				Type: notionapi.ObjectTypeText,
				Text: &notionapi.Text{
					Content: title,
				},
			},
		},
		Properties: notionapi.PropertyConfigs{
			"title": notionapi.TitlePropertyConfig{
				Type: notionapi.PropertyConfigTypeTitle,
			},
			markdownFileProperty: notionapi.RichTextPropertyConfig{
				Type: notionapi.PropertyConfigTypeRichText,
			},
			contentHashProperty: notionapi.RichTextPropertyConfig{
				Type: notionapi.PropertyConfigTypeRichText,
			},
		},
	}

	db, err := c.api.Database.Create(ctx, request)
	if err != nil {
		return "", fmt.Errorf("failed to create child database: %w", err)
	}

	// Mark this database as having its properties ensured
	c.ensuredDatabasesMutex.Lock()
	c.ensuredDatabases[string(db.ID)] = true
	c.ensuredDatabasesMutex.Unlock()

	return string(db.ID), nil
}

// FindChildDatabase finds a child database by title within a parent page
func (c *Client) FindChildDatabase(parentPageID, title string) (string, bool, error) {
	ctx := context.Background()

	// Get all child blocks
	blocks, err := c.api.Block.GetChildren(ctx, notionapi.BlockID(parentPageID), nil)
	if err != nil {
		return "", false, fmt.Errorf("failed to get child blocks: %w", err)
	}

	for _, block := range blocks.Results {
		if block.GetType() == notionapi.BlockTypeChildDatabase {
			// Get database details
			dbBlock, ok := block.(*notionapi.ChildDatabaseBlock)
			if ok && dbBlock.ChildDatabase.Title == title {
				return string(block.GetID()), true, nil
			}
		}
	}

	return "", false, nil
}

// GetDatabaseEntries retrieves all entries from a database
func (c *Client) GetDatabaseEntries(databaseID string) ([]notionapi.Page, error) {
	ctx := context.Background()

	var allPages []notionapi.Page
	var cursor notionapi.Cursor

	for {
		query := &notionapi.DatabaseQueryRequest{}
		if cursor != "" {
			query.StartCursor = cursor
		}

		result, err := c.api.Database.Query(ctx, notionapi.DatabaseID(databaseID), query)
		if err != nil {
			return nil, fmt.Errorf("failed to query database: %w", err)
		}

		allPages = append(allPages, result.Results...)

		if !result.HasMore {
			break
		}
		cursor = notionapi.Cursor(result.NextCursor)
	}

	return allPages, nil
}

// DeleteEntry archives a database entry
func (c *Client) DeleteEntry(pageID string) error {
	ctx := context.Background()

	updateRequest := &notionapi.PageUpdateRequest{
		Archived: true,
	}

	_, err := c.api.Page.Update(ctx, notionapi.PageID(pageID), updateRequest)
	if err != nil {
		return fmt.Errorf("failed to archive entry: %w", err)
	}

	return nil
}

// BuildFilenameToPageMap builds a map of markdown filename to Notion page ID for a database
func (c *Client) BuildFilenameToPageMap(databaseID string) (map[string]string, error) {
	entries, err := c.GetDatabaseEntries(databaseID)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, entry := range entries {
		if prop, ok := entry.Properties["Markdown File"]; ok {
			if richTextProp, ok := prop.(*notionapi.RichTextProperty); ok {
				if len(richTextProp.RichText) > 0 {
					filename := richTextProp.RichText[0].PlainText
					result[filename] = string(entry.ID)
				}
			}
		}
	}

	return result, nil
}

// GetNotionPageURL returns the URL for a Notion page
func GetNotionPageURL(pageID string) string {
	// Remove dashes from page ID for URL
	cleanID := strings.ReplaceAll(pageID, "-", "")
	return fmt.Sprintf("https://notion.so/%s", cleanID)
}
