package notion

import (
	"context"
	"fmt"
	"os"

	"github.com/jomei/notionapi"
)

// Client wraps the Notion API client
type Client struct {
	api *notionapi.Client
}

// NewClient creates a new Notion client
func NewClient(token string) (*Client, error) {
	if token == "" {
		return nil, fmt.Errorf("notion token is required")
	}

	client := notionapi.NewClient(notionapi.Token(token))
	return &Client{api: client}, nil
}

// CreatePage creates a new page under the specified parent with the given title, filename, and blocks
func (c *Client) CreatePage(parentPageID, title, filename string, blocks []notionapi.Block) (string, error) {
	ctx := context.Background()

	// Create the page request
	request := &notionapi.PageCreateRequest{
		Parent: notionapi.Parent{
			Type:   notionapi.ParentTypePageID,
			PageID: notionapi.PageID(parentPageID),
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
			"Markdown File": notionapi.RichTextProperty{
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
		},
		Children: blocks,
	}

	page, err := c.api.Page.Create(ctx, request)
	if err != nil {
		return "", fmt.Errorf("failed to create page: %w", err)
	}

	return string(page.ID), nil
}

// UpdatePage updates an existing page with new title, filename, and blocks
func (c *Client) UpdatePage(pageID, title, filename string, blocks []notionapi.Block) error {
	ctx := context.Background()

	// First, update the page title and filename
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
			"Markdown File": notionapi.RichTextProperty{
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
		},
	}

	_, err := c.api.Page.Update(ctx, notionapi.PageID(pageID), updateRequest)
	if err != nil {
		return fmt.Errorf("failed to update page title: %w", err)
	}

	// Get existing blocks to delete them
	existingBlocks, err := c.api.Block.GetChildren(ctx, notionapi.BlockID(pageID), nil)
	if err != nil {
		return fmt.Errorf("failed to get existing blocks: %w", err)
	}

	// Delete all existing blocks
	// Note: Deletion is sequential as the Notion API doesn't support batch deletion
	for _, block := range existingBlocks.Results {
		_, err = c.api.Block.Delete(ctx, block.GetID())
		if err != nil {
			// Log the error but continue trying to delete other blocks
			// This ensures partial cleanup even if some blocks fail
			fmt.Fprintf(os.Stderr, "Warning: failed to delete block %s: %v\n", block.GetID(), err)
		}
	}

	// Add new blocks
	if len(blocks) > 0 {
		appendRequest := &notionapi.AppendBlockChildrenRequest{
			Children: blocks,
		}
		_, err = c.api.Block.AppendChildren(ctx, notionapi.BlockID(pageID), appendRequest)
		if err != nil {
			return fmt.Errorf("failed to append new blocks: %w", err)
		}
	}

	return nil
}

// GetChildPages retrieves all child pages of a parent page
func (c *Client) GetChildPages(parentPageID string) ([]notionapi.Page, error) {
	ctx := context.Background()

	// Get all child blocks
	blocks, err := c.api.Block.GetChildren(ctx, notionapi.BlockID(parentPageID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get child blocks: %w", err)
	}

	var pages []notionapi.Page
	for _, block := range blocks.Results {
		// Check if this block is a child page
		if block.GetType() == notionapi.BlockTypeChildPage {
			// Get the full page details
			pageID := block.GetID()
			page, err := c.api.Page.Get(ctx, notionapi.PageID(pageID))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to get page %s: %v\n", pageID, err)
				continue
			}
			pages = append(pages, *page)
		}
	}

	return pages, nil
}

// FindPageByFilename finds a child page by its markdown filename property
func (c *Client) FindPageByFilename(parentPageID, filename string) (string, bool, error) {
	pages, err := c.GetChildPages(parentPageID)
	if err != nil {
		return "", false, err
	}

	for _, page := range pages {
		// Check if page has the "Markdown File" property
		if prop, ok := page.Properties["Markdown File"]; ok {
			if richTextProp, ok := prop.(*notionapi.RichTextProperty); ok {
				if len(richTextProp.RichText) > 0 {
					pageFilename := richTextProp.RichText[0].PlainText
					if pageFilename == filename {
						return string(page.ID), true, nil
					}
				}
			}
		}
	}

	return "", false, nil
}

// DeletePage deletes a page
func (c *Client) DeletePage(pageID string) error {
	ctx := context.Background()

	// Archive the page (Notion doesn't support permanent deletion via API)
	updateRequest := &notionapi.PageUpdateRequest{
		Archived: true,
	}

	_, err := c.api.Page.Update(ctx, notionapi.PageID(pageID), updateRequest)
	if err != nil {
		return fmt.Errorf("failed to archive page: %w", err)
	}

	return nil
}
