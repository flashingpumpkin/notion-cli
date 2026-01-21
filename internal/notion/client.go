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

// CreatePage creates a new page under the specified parent with the given title and blocks
func (c *Client) CreatePage(parentPageID, title string, blocks []notionapi.Block) (string, error) {
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
		},
		Children: blocks,
	}

	page, err := c.api.Page.Create(ctx, request)
	if err != nil {
		return "", fmt.Errorf("failed to create page: %w", err)
	}

	return string(page.ID), nil
}

// UpdatePage updates an existing page with new title and blocks
func (c *Client) UpdatePage(pageID, title string, blocks []notionapi.Block) error {
	ctx := context.Background()

	// First, update the page title
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
