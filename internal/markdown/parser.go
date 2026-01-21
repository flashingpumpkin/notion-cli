package markdown

import (
	"bytes"
	"strings"

	"github.com/jomei/notionapi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Document represents a parsed markdown document
type Document struct {
	Title  string
	Blocks []notionapi.Block
}

// Parse parses markdown content and returns a Document
func Parse(content []byte) (*Document, error) {
	md := goldmark.New()
	reader := text.NewReader(content)
	doc := md.Parser().Parse(reader)

	var title string
	var blocks []notionapi.Block

	// Extract title (first heading) and convert to Notion blocks
	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch node := n.(type) {
		case *ast.Heading:
			if title == "" && node.Level == 1 {
				// Extract title from first H1
				title = extractText(node, content)
				return ast.WalkSkipChildren, nil
			}
			// Convert other headings to blocks
			block := convertHeading(node, content)
			if block != nil {
				blocks = append(blocks, block)
			}
			return ast.WalkSkipChildren, nil

		case *ast.Paragraph:
			// Skip if this is part of a heading
			if _, ok := n.Parent().(*ast.Heading); ok {
				return ast.WalkContinue, nil
			}
			block := convertParagraph(node, content)
			if block != nil {
				blocks = append(blocks, block)
			}
			return ast.WalkSkipChildren, nil

		case *ast.List:
			// Convert lists
			listBlocks := convertList(node, content)
			blocks = append(blocks, listBlocks...)
			return ast.WalkSkipChildren, nil

		case *ast.FencedCodeBlock, *ast.CodeBlock:
			block := convertCodeBlock(node, content)
			if block != nil {
				blocks = append(blocks, block)
			}
			return ast.WalkSkipChildren, nil
		}

		return ast.WalkContinue, nil
	})

	if err != nil {
		return nil, err
	}

	// Default title if none found
	if title == "" {
		title = "Untitled"
	}

	return &Document{
		Title:  title,
		Blocks: blocks,
	}, nil
}

// extractText extracts text content from a node
func extractText(node ast.Node, source []byte) string {
	var buf bytes.Buffer
	for c := node.FirstChild(); c != nil; c = c.NextSibling() {
		if text, ok := c.(*ast.Text); ok {
			buf.Write(text.Segment.Value(source))
		}
	}
	return buf.String()
}

// convertHeading converts a markdown heading to a Notion block
func convertHeading(node *ast.Heading, source []byte) notionapi.Block {
	text := extractText(node, source)
	if text == "" {
		return nil
	}

	richText := []notionapi.RichText{
		{
			Type: notionapi.ObjectTypeText,
			Text: &notionapi.Text{
				Content: text,
			},
		},
	}

	switch node.Level {
	case 1:
		return &notionapi.Heading1Block{
			BasicBlock: notionapi.BasicBlock{
				Object: notionapi.ObjectTypeBlock,
				Type:   notionapi.BlockTypeHeading1,
			},
			Heading1: notionapi.Heading{
				RichText: richText,
			},
		}
	case 2:
		return &notionapi.Heading2Block{
			BasicBlock: notionapi.BasicBlock{
				Object: notionapi.ObjectTypeBlock,
				Type:   notionapi.BlockTypeHeading2,
			},
			Heading2: notionapi.Heading{
				RichText: richText,
			},
		}
	case 3:
		return &notionapi.Heading3Block{
			BasicBlock: notionapi.BasicBlock{
				Object: notionapi.ObjectTypeBlock,
				Type:   notionapi.BlockTypeHeading3,
			},
			Heading3: notionapi.Heading{
				RichText: richText,
			},
		}
	default:
		return &notionapi.Heading3Block{
			BasicBlock: notionapi.BasicBlock{
				Object: notionapi.ObjectTypeBlock,
				Type:   notionapi.BlockTypeHeading3,
			},
			Heading3: notionapi.Heading{
				RichText: richText,
			},
		}
	}
}

// convertParagraph converts a markdown paragraph to a Notion block
func convertParagraph(node *ast.Paragraph, source []byte) notionapi.Block {
	text := extractText(node, source)
	if text == "" {
		return nil
	}

	return &notionapi.ParagraphBlock{
		BasicBlock: notionapi.BasicBlock{
			Object: notionapi.ObjectTypeBlock,
			Type:   notionapi.BlockTypeParagraph,
		},
		Paragraph: notionapi.Paragraph{
			RichText: []notionapi.RichText{
				{
					Type: notionapi.ObjectTypeText,
					Text: &notionapi.Text{
						Content: text,
					},
				},
			},
		},
	}
}

// convertList converts a markdown list to Notion blocks
func convertList(node *ast.List, source []byte) []notionapi.Block {
	var blocks []notionapi.Block

	for c := node.FirstChild(); c != nil; c = c.NextSibling() {
		if item, ok := c.(*ast.ListItem); ok {
			text := extractText(item, source)
			if text == "" {
				continue
			}

			if node.IsOrdered() {
				blocks = append(blocks, &notionapi.NumberedListItemBlock{
					BasicBlock: notionapi.BasicBlock{
						Object: notionapi.ObjectTypeBlock,
						Type:   notionapi.BlockTypeNumberedListItem,
					},
					NumberedListItem: notionapi.ListItem{
						RichText: []notionapi.RichText{
							{
								Type: notionapi.ObjectTypeText,
								Text: &notionapi.Text{
									Content: text,
								},
							},
						},
					},
				})
			} else {
				blocks = append(blocks, &notionapi.BulletedListItemBlock{
					BasicBlock: notionapi.BasicBlock{
						Object: notionapi.ObjectTypeBlock,
						Type:   notionapi.BlockTypeBulletedListItem,
					},
					BulletedListItem: notionapi.ListItem{
						RichText: []notionapi.RichText{
							{
								Type: notionapi.ObjectTypeText,
								Text: &notionapi.Text{
									Content: text,
								},
							},
						},
					},
				})
			}
		}
	}

	return blocks
}

// convertCodeBlock converts a markdown code block to a Notion block
func convertCodeBlock(node ast.Node, source []byte) notionapi.Block {
	var language string
	var content string

	if fenced, ok := node.(*ast.FencedCodeBlock); ok {
		language = string(fenced.Language(source))
		content = extractCodeContent(fenced, source)
	} else if code, ok := node.(*ast.CodeBlock); ok {
		language = "plain text"
		content = extractCodeContent(code, source)
	}

	if content == "" {
		return nil
	}

	return &notionapi.CodeBlock{
		BasicBlock: notionapi.BasicBlock{
			Object: notionapi.ObjectTypeBlock,
			Type:   notionapi.BlockTypeCode,
		},
		Code: notionapi.Code{
			RichText: []notionapi.RichText{
				{
					Type: notionapi.ObjectTypeText,
					Text: &notionapi.Text{
						Content: content,
					},
				},
			},
			Language: language,
		},
	}
}

// extractCodeContent extracts content from a code block
func extractCodeContent(node ast.Node, source []byte) string {
	var buf bytes.Buffer
	lines := node.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		buf.Write(line.Value(source))
	}
	return strings.TrimRight(buf.String(), "\n")
}
