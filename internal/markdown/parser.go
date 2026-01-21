package markdown

import (
	"bytes"
	"path/filepath"
	"strings"

	"github.com/jomei/notionapi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

// Document represents a parsed markdown document
type Document struct {
	Title  string
	Blocks []notionapi.Block
}

// ResolveRelativeLinks updates relative markdown links in blocks to Notion page URLs
// linkMap maps markdown filenames (e.g., "file.md") to Notion page URLs
func ResolveRelativeLinks(blocks []notionapi.Block, linkMap map[string]string) {
	for _, block := range blocks {
		resolveBlockLinks(block, linkMap)
	}
}

func resolveBlockLinks(block notionapi.Block, linkMap map[string]string) {
	switch b := block.(type) {
	case *notionapi.ParagraphBlock:
		resolveRichTextLinks(b.Paragraph.RichText, linkMap)
		ResolveRelativeLinks(b.Paragraph.Children, linkMap)
	case *notionapi.Heading1Block:
		resolveRichTextLinks(b.Heading1.RichText, linkMap)
	case *notionapi.Heading2Block:
		resolveRichTextLinks(b.Heading2.RichText, linkMap)
	case *notionapi.Heading3Block:
		resolveRichTextLinks(b.Heading3.RichText, linkMap)
	case *notionapi.BulletedListItemBlock:
		resolveRichTextLinks(b.BulletedListItem.RichText, linkMap)
		ResolveRelativeLinks(b.BulletedListItem.Children, linkMap)
	case *notionapi.NumberedListItemBlock:
		resolveRichTextLinks(b.NumberedListItem.RichText, linkMap)
		ResolveRelativeLinks(b.NumberedListItem.Children, linkMap)
	case *notionapi.ToDoBlock:
		resolveRichTextLinks(b.ToDo.RichText, linkMap)
		ResolveRelativeLinks(b.ToDo.Children, linkMap)
	case *notionapi.QuoteBlock:
		resolveRichTextLinks(b.Quote.RichText, linkMap)
		ResolveRelativeLinks(b.Quote.Children, linkMap)
	case *notionapi.TableBlock:
		ResolveRelativeLinks(b.Table.Children, linkMap)
	case *notionapi.TableRowBlock:
		for _, cell := range b.TableRow.Cells {
			resolveRichTextLinks(cell, linkMap)
		}
	}
}

func resolveRichTextLinks(richTexts []notionapi.RichText, linkMap map[string]string) {
	for i := range richTexts {
		if richTexts[i].Text != nil && richTexts[i].Text.Link != nil {
			url := richTexts[i].Text.Link.Url
			// Check if it's a relative markdown link
			if isRelativeMarkdownLink(url) {
				filename := filepath.Base(url)
				if pageID, ok := linkMap[filename]; ok {
					// Convert to page mention for internal navigation
					linkText := richTexts[i].Text.Content
					richTexts[i] = notionapi.RichText{
						Type: notionapi.ObjectType("mention"),
						Mention: &notionapi.Mention{
							Type: notionapi.MentionTypePage,
							Page: &notionapi.PageMention{
								ID: notionapi.ObjectID(pageID),
							},
						},
						PlainText:   linkText,
						Annotations: richTexts[i].Annotations,
					}
				}
			}
		}
	}
}

func isRelativeMarkdownLink(url string) bool {
	// Check for relative paths to .md files
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return false
	}
	return strings.HasSuffix(strings.ToLower(url), ".md")
}

// Parse parses markdown content and returns a Document
func Parse(content []byte) (*Document, error) {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.Table,
			extension.Strikethrough,
			extension.TaskList,
		),
	)
	reader := text.NewReader(content)
	doc := md.Parser().Parse(reader)

	var title string
	var fallbackTitle string
	var blocks []notionapi.Block

	// Walk all direct children of the document
	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		// Check if this block has blank lines before it - if so, insert an empty paragraph
		if blockNode, ok := child.(ast.Node); ok {
			if hasBlankPreviousLines(blockNode) {
				blocks = append(blocks, createEmptyParagraph())
			}
		}

		block := convertNode(child, content, &title, &fallbackTitle)
		if block != nil {
			blocks = append(blocks, block...)
		}
	}

	// Use fallback title (first H2) if no H1 found
	if title == "" && fallbackTitle != "" {
		title = fallbackTitle
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

// hasBlankPreviousLines checks if a node has blank lines before it
func hasBlankPreviousLines(node ast.Node) bool {
	// Use type assertion to access HasBlankPreviousLines method
	type blankLinesChecker interface {
		HasBlankPreviousLines() bool
	}
	if checker, ok := node.(blankLinesChecker); ok {
		return checker.HasBlankPreviousLines()
	}
	return false
}

// createEmptyParagraph creates an empty paragraph block for visual spacing
func createEmptyParagraph() notionapi.Block {
	return &notionapi.ParagraphBlock{
		BasicBlock: notionapi.BasicBlock{
			Object: notionapi.ObjectTypeBlock,
			Type:   notionapi.BlockTypeParagraph,
		},
		Paragraph: notionapi.Paragraph{
			RichText: []notionapi.RichText{},
		},
	}
}

// convertNode converts any AST node to Notion blocks
func convertNode(node ast.Node, source []byte, title *string, fallbackTitle *string) []notionapi.Block {
	switch n := node.(type) {
	case *ast.Heading:
		// Extract title from first H1 found anywhere in the document
		if *title == "" && n.Level == 1 {
			*title = extractPlainText(n, source)
			return nil // Don't include H1 as a block if it's the title
		}
		// Use first H2 as fallback title if no H1
		if *fallbackTitle == "" && n.Level == 2 {
			*fallbackTitle = extractPlainText(n, source)
			// Still include H2 as a block (unlike H1 which becomes page title)
		}
		block := convertHeading(n, source)
		if block != nil {
			return []notionapi.Block{block}
		}

	case *ast.Paragraph:
		// Check if paragraph contains only an image
		if img := findImage(n); img != nil {
			block := convertImage(img, source)
			if block != nil {
				return []notionapi.Block{block}
			}
		}
		block := convertParagraph(n, source)
		if block != nil {
			return []notionapi.Block{block}
		}

	case *ast.List:
		return convertList(n, source)

	case *ast.FencedCodeBlock, *ast.CodeBlock:
		block := convertCodeBlock(n, source)
		if block != nil {
			return []notionapi.Block{block}
		}

	case *ast.Blockquote:
		block := convertBlockquote(n, source)
		if block != nil {
			return []notionapi.Block{block}
		}

	case *ast.ThematicBreak:
		return []notionapi.Block{
			&notionapi.DividerBlock{
				BasicBlock: notionapi.BasicBlock{
					Object: notionapi.ObjectTypeBlock,
					Type:   notionapi.BlockTypeDivider,
				},
			},
		}

	case *east.Table:
		block := convertTable(n, source)
		if block != nil {
			return []notionapi.Block{block}
		}
	}

	return nil
}

// findImage checks if a paragraph contains only an image
func findImage(p *ast.Paragraph) *ast.Image {
	var img *ast.Image
	count := 0
	for c := p.FirstChild(); c != nil; c = c.NextSibling() {
		count++
		if image, ok := c.(*ast.Image); ok {
			img = image
		}
	}
	if count == 1 && img != nil {
		return img
	}
	return nil
}

// extractPlainText extracts plain text content from a node (no formatting)
func extractPlainText(node ast.Node, source []byte) string {
	var buf bytes.Buffer

	ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		if t, ok := n.(*ast.Text); ok {
			buf.Write(t.Segment.Value(source))
			if t.SoftLineBreak() {
				buf.WriteByte(' ')
			}
		}

		return ast.WalkContinue, nil
	})

	return strings.TrimSpace(buf.String())
}

// extractRichText extracts rich text with formatting from a node
func extractRichText(node ast.Node, source []byte) []notionapi.RichText {
	var richTexts []notionapi.RichText

	var walk func(n ast.Node, annotations *notionapi.Annotations)
	walk = func(n ast.Node, annotations *notionapi.Annotations) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			// Clone annotations for this branch
			ann := &notionapi.Annotations{}
			if annotations != nil {
				*ann = *annotations
			}

			switch child := c.(type) {
			case *ast.Text:
				text := string(child.Segment.Value(source))
				if text == "" {
					continue
				}
				rt := notionapi.RichText{
					Type: notionapi.ObjectTypeText,
					Text: &notionapi.Text{
						Content: text,
					},
				}
				if ann.Bold || ann.Italic || ann.Strikethrough || ann.Code {
					rt.Annotations = ann
				}
				richTexts = append(richTexts, rt)
				if child.SoftLineBreak() {
					richTexts = append(richTexts, notionapi.RichText{
						Type: notionapi.ObjectTypeText,
						Text: &notionapi.Text{Content: " "},
					})
				}

			case *ast.Emphasis:
				newAnn := &notionapi.Annotations{}
				if annotations != nil {
					*newAnn = *annotations
				}
				if child.Level == 1 {
					newAnn.Italic = true
				} else if child.Level >= 2 {
					newAnn.Bold = true
				}
				walk(child, newAnn)

			case *ast.CodeSpan:
				newAnn := &notionapi.Annotations{}
				if annotations != nil {
					*newAnn = *annotations
				}
				newAnn.Code = true
				text := extractPlainText(child, source)
				if text != "" {
					richTexts = append(richTexts, notionapi.RichText{
						Type: notionapi.ObjectTypeText,
						Text: &notionapi.Text{Content: text},
						Annotations: newAnn,
					})
				}

			case *east.Strikethrough:
				newAnn := &notionapi.Annotations{}
				if annotations != nil {
					*newAnn = *annotations
				}
				newAnn.Strikethrough = true
				walk(child, newAnn)

			case *ast.Link:
				text := extractPlainText(child, source)
				url := string(child.Destination)
				if text != "" {
					rt := notionapi.RichText{
						Type: notionapi.ObjectTypeText,
						Text: &notionapi.Text{
							Content: text,
							Link:    &notionapi.Link{Url: url},
						},
					}
					if ann.Bold || ann.Italic || ann.Strikethrough || ann.Code {
						rt.Annotations = ann
					}
					richTexts = append(richTexts, rt)
				}

			case *ast.AutoLink:
				url := string(child.URL(source))
				richTexts = append(richTexts, notionapi.RichText{
					Type: notionapi.ObjectTypeText,
					Text: &notionapi.Text{
						Content: url,
						Link:    &notionapi.Link{Url: url},
					},
				})

			default:
				// Recurse for other nodes
				walk(child, ann)
			}
		}
	}

	walk(node, nil)
	return richTexts
}

// convertHeading converts a markdown heading to a Notion block
func convertHeading(node *ast.Heading, source []byte) notionapi.Block {
	richText := extractRichText(node, source)
	if len(richText) == 0 {
		return nil
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
	richText := extractRichText(node, source)
	if len(richText) == 0 {
		return nil
	}

	return &notionapi.ParagraphBlock{
		BasicBlock: notionapi.BasicBlock{
			Object: notionapi.ObjectTypeBlock,
			Type:   notionapi.BlockTypeParagraph,
		},
		Paragraph: notionapi.Paragraph{
			RichText: richText,
		},
	}
}

// convertList converts a markdown list to Notion blocks
func convertList(node *ast.List, source []byte) []notionapi.Block {
	var blocks []notionapi.Block

	for c := node.FirstChild(); c != nil; c = c.NextSibling() {
		item, ok := c.(*ast.ListItem)
		if !ok {
			continue
		}

		// Extract rich text and nested children from the list item
		richText, children := extractListItemContent(item, source)

		// Skip items with no content at all
		if len(richText) == 0 && len(children) == 0 {
			continue
		}

		// Check if first child is a task checkbox
		var isTask bool
		var isChecked bool
		for fc := item.FirstChild(); fc != nil; fc = fc.NextSibling() {
			if para, ok := fc.(*ast.Paragraph); ok {
				if firstChild := para.FirstChild(); firstChild != nil {
					if cb, ok := firstChild.(*east.TaskCheckBox); ok {
						isTask = true
						isChecked = cb.IsChecked
						break
					}
				}
			}
		}

		if isTask {
			blocks = append(blocks, &notionapi.ToDoBlock{
				BasicBlock: notionapi.BasicBlock{
					Object: notionapi.ObjectTypeBlock,
					Type:   notionapi.BlockTypeToDo,
				},
				ToDo: notionapi.ToDo{
					RichText: richText,
					Checked:  isChecked,
					Children: children,
				},
			})
		} else if node.IsOrdered() {
			blocks = append(blocks, &notionapi.NumberedListItemBlock{
				BasicBlock: notionapi.BasicBlock{
					Object: notionapi.ObjectTypeBlock,
					Type:   notionapi.BlockTypeNumberedListItem,
				},
				NumberedListItem: notionapi.ListItem{
					RichText: richText,
					Children: children,
				},
			})
		} else {
			blocks = append(blocks, &notionapi.BulletedListItemBlock{
				BasicBlock: notionapi.BasicBlock{
					Object: notionapi.ObjectTypeBlock,
					Type:   notionapi.BlockTypeBulletedListItem,
				},
				BulletedListItem: notionapi.ListItem{
					RichText: richText,
					Children: children,
				},
			})
		}
	}

	return blocks
}

// extractListItemContent extracts rich text and nested children from a list item
func extractListItemContent(item *ast.ListItem, source []byte) ([]notionapi.RichText, notionapi.Blocks) {
	var richTexts []notionapi.RichText
	var children notionapi.Blocks

	for c := item.FirstChild(); c != nil; c = c.NextSibling() {
		switch child := c.(type) {
		case *ast.Paragraph:
			// Check for task checkbox first
			if fc := child.FirstChild(); fc != nil {
				if _, ok := fc.(*east.TaskCheckBox); ok {
					// Extract text after checkbox
					for pc := fc.NextSibling(); pc != nil; pc = pc.NextSibling() {
						richTexts = append(richTexts, extractRichTextFromNode(pc, source, nil)...)
					}
					continue
				}
			}
			// Use extractRichText for robust text extraction
			richTexts = append(richTexts, extractRichText(child, source)...)

		case *ast.TextBlock:
			// TextBlock can appear in tight lists
			richTexts = append(richTexts, extractRichText(child, source)...)

		case *ast.List:
			// Nested list - convert recursively
			nestedBlocks := convertList(child, source)
			children = append(children, nestedBlocks...)
		}
	}

	return richTexts, children
}

// extractRichTextFromNode extracts rich text from a single node
func extractRichTextFromNode(node ast.Node, source []byte, annotations *notionapi.Annotations) []notionapi.RichText {
	var richTexts []notionapi.RichText

	ann := &notionapi.Annotations{}
	if annotations != nil {
		*ann = *annotations
	}

	switch n := node.(type) {
	case *ast.Text:
		text := string(n.Segment.Value(source))
		if text == "" {
			return nil
		}
		rt := notionapi.RichText{
			Type: notionapi.ObjectTypeText,
			Text: &notionapi.Text{Content: text},
		}
		if ann.Bold || ann.Italic || ann.Strikethrough || ann.Code {
			rt.Annotations = ann
		}
		richTexts = append(richTexts, rt)
		if n.SoftLineBreak() {
			richTexts = append(richTexts, notionapi.RichText{
				Type: notionapi.ObjectTypeText,
				Text: &notionapi.Text{Content: " "},
			})
		}

	case *ast.Emphasis:
		newAnn := &notionapi.Annotations{}
		if annotations != nil {
			*newAnn = *annotations
		}
		if n.Level == 1 {
			newAnn.Italic = true
		} else {
			newAnn.Bold = true
		}
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			richTexts = append(richTexts, extractRichTextFromNode(c, source, newAnn)...)
		}

	case *ast.CodeSpan:
		newAnn := &notionapi.Annotations{Code: true}
		if annotations != nil {
			newAnn.Bold = annotations.Bold
			newAnn.Italic = annotations.Italic
			newAnn.Strikethrough = annotations.Strikethrough
		}
		text := extractPlainText(n, source)
		if text != "" {
			richTexts = append(richTexts, notionapi.RichText{
				Type:        notionapi.ObjectTypeText,
				Text:        &notionapi.Text{Content: text},
				Annotations: newAnn,
			})
		}

	case *east.Strikethrough:
		newAnn := &notionapi.Annotations{Strikethrough: true}
		if annotations != nil {
			newAnn.Bold = annotations.Bold
			newAnn.Italic = annotations.Italic
			newAnn.Code = annotations.Code
		}
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			richTexts = append(richTexts, extractRichTextFromNode(c, source, newAnn)...)
		}

	case *ast.Link:
		text := extractPlainText(n, source)
		url := string(n.Destination)
		if text != "" {
			rt := notionapi.RichText{
				Type: notionapi.ObjectTypeText,
				Text: &notionapi.Text{
					Content: text,
					Link:    &notionapi.Link{Url: url},
				},
			}
			if ann.Bold || ann.Italic || ann.Strikethrough || ann.Code {
				rt.Annotations = ann
			}
			richTexts = append(richTexts, rt)
		}

	case *ast.AutoLink:
		url := string(n.URL(source))
		richTexts = append(richTexts, notionapi.RichText{
			Type: notionapi.ObjectTypeText,
			Text: &notionapi.Text{
				Content: url,
				Link:    &notionapi.Link{Url: url},
			},
		})

	default:
		for c := node.FirstChild(); c != nil; c = c.NextSibling() {
			richTexts = append(richTexts, extractRichTextFromNode(c, source, ann)...)
		}
	}

	return richTexts
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

	if language == "" {
		language = "plain text"
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
					Text: &notionapi.Text{Content: content},
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

// convertBlockquote converts a markdown blockquote to a Notion block
func convertBlockquote(node *ast.Blockquote, source []byte) notionapi.Block {
	var richTexts []notionapi.RichText

	for c := node.FirstChild(); c != nil; c = c.NextSibling() {
		if para, ok := c.(*ast.Paragraph); ok {
			richTexts = append(richTexts, extractRichText(para, source)...)
		}
	}

	if len(richTexts) == 0 {
		return nil
	}

	return &notionapi.QuoteBlock{
		BasicBlock: notionapi.BasicBlock{
			Object: notionapi.ObjectTypeBlock,
			Type:   notionapi.BlockTypeQuote,
		},
		Quote: notionapi.Quote{
			RichText: richTexts,
		},
	}
}

// convertImage converts a markdown image to a Notion block
func convertImage(node *ast.Image, source []byte) notionapi.Block {
	url := string(node.Destination)
	if url == "" {
		return nil
	}

	caption := extractPlainText(node, source)
	var captionRichText []notionapi.RichText
	if caption != "" {
		captionRichText = []notionapi.RichText{
			{
				Type: notionapi.ObjectTypeText,
				Text: &notionapi.Text{Content: caption},
			},
		}
	}

	return &notionapi.ImageBlock{
		BasicBlock: notionapi.BasicBlock{
			Object: notionapi.ObjectTypeBlock,
			Type:   notionapi.BlockTypeImage,
		},
		Image: notionapi.Image{
			Type: notionapi.FileTypeExternal,
			External: &notionapi.FileObject{
				URL: url,
			},
			Caption: captionRichText,
		},
	}
}

// convertTable converts a markdown table to a Notion table block
func convertTable(node *east.Table, source []byte) notionapi.Block {
	var rows []notionapi.Block
	var columnCount int

	for c := node.FirstChild(); c != nil; c = c.NextSibling() {
		switch row := c.(type) {
		case *east.TableHeader:
			// TableHeader contains TableCell directly (not wrapped in TableRow)
			cells := extractTableHeaderCells(row, source)
			if columnCount == 0 {
				columnCount = len(cells)
			}
			if len(cells) > 0 {
				rows = append(rows, &notionapi.TableRowBlock{
					BasicBlock: notionapi.BasicBlock{
						Object: notionapi.ObjectTypeBlock,
						Type:   notionapi.BlockTypeTableRowBlock,
					},
					TableRow: notionapi.TableRow{
						Cells: cells,
					},
				})
			}
		case *east.TableRow:
			cells := extractTableCells(row, source)
			if columnCount == 0 {
				columnCount = len(cells)
			}
			rows = append(rows, &notionapi.TableRowBlock{
				BasicBlock: notionapi.BasicBlock{
					Object: notionapi.ObjectTypeBlock,
					Type:   notionapi.BlockTypeTableRowBlock,
				},
				TableRow: notionapi.TableRow{
					Cells: cells,
				},
			})
		}
	}

	if len(rows) == 0 || columnCount == 0 {
		return nil
	}

	return &notionapi.TableBlock{
		BasicBlock: notionapi.BasicBlock{
			Object: notionapi.ObjectTypeBlock,
			Type:   notionapi.BlockTypeTableBlock,
		},
		Table: notionapi.Table{
			TableWidth:      columnCount,
			HasColumnHeader: true,
			HasRowHeader:    false,
			Children:        rows,
		},
	}
}

// extractTableHeaderCells extracts cells directly from a TableHeader node
func extractTableHeaderCells(header *east.TableHeader, source []byte) [][]notionapi.RichText {
	var cells [][]notionapi.RichText

	for c := header.FirstChild(); c != nil; c = c.NextSibling() {
		if cell, ok := c.(*east.TableCell); ok {
			richText := extractRichText(cell, source)
			if len(richText) == 0 {
				richText = []notionapi.RichText{{
					Type: notionapi.ObjectTypeText,
					Text: &notionapi.Text{Content: ""},
				}}
			}
			cells = append(cells, richText)
		}
	}

	return cells
}

// extractTableCells extracts cells from a table row
func extractTableCells(row *east.TableRow, source []byte) [][]notionapi.RichText {
	var cells [][]notionapi.RichText

	for c := row.FirstChild(); c != nil; c = c.NextSibling() {
		if cell, ok := c.(*east.TableCell); ok {
			richText := extractRichText(cell, source)
			if len(richText) == 0 {
				richText = []notionapi.RichText{{
					Type: notionapi.ObjectTypeText,
					Text: &notionapi.Text{Content: ""},
				}}
			}
			cells = append(cells, richText)
		}
	}

	return cells
}
