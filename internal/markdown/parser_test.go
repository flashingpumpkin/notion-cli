package markdown

import (
	"testing"

	"github.com/jomei/notionapi"
)

func TestParseBasicMarkdown(t *testing.T) {
	content := []byte(`# Test Title

This is a paragraph.

## Section 1

Another paragraph here.`)

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("Failed to parse markdown: %v", err)
	}

	if doc.Title != "Test Title" {
		t.Errorf("Expected title 'Test Title', got '%s'", doc.Title)
	}

	if len(doc.Blocks) == 0 {
		t.Error("Expected blocks to be parsed")
	}
}

func TestParseWithLists(t *testing.T) {
	content := []byte(`# List Test

## Unordered List

- Item 1
- Item 2
- Item 3

## Ordered List

1. First
2. Second
3. Third`)

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("Failed to parse markdown: %v", err)
	}

	if doc.Title != "List Test" {
		t.Errorf("Expected title 'List Test', got '%s'", doc.Title)
	}

	// Check for list items
	foundBullet := false
	foundNumbered := false

	for _, block := range doc.Blocks {
		switch block.(type) {
		case *notionapi.BulletedListItemBlock:
			foundBullet = true
		case *notionapi.NumberedListItemBlock:
			foundNumbered = true
		}
	}

	if !foundBullet {
		t.Error("Expected to find bulleted list items")
	}

	if !foundNumbered {
		t.Error("Expected to find numbered list items")
	}
}

func TestParseWithCodeBlock(t *testing.T) {
	content := []byte(`# Code Test

Here is some code:

` + "```go" + `
func main() {
    fmt.Println("Hello")
}
` + "```" + `

End of document.`)

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("Failed to parse markdown: %v", err)
	}

	if doc.Title != "Code Test" {
		t.Errorf("Expected title 'Code Test', got '%s'", doc.Title)
	}

	// Check for code block
	foundCode := false
	for _, block := range doc.Blocks {
		if _, ok := block.(*notionapi.CodeBlock); ok {
			foundCode = true
			break
		}
	}

	if !foundCode {
		t.Error("Expected to find code block")
	}
}

func TestParseEmptyTitle(t *testing.T) {
	content := []byte(`This is just a paragraph without a title.`)

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("Failed to parse markdown: %v", err)
	}

	if doc.Title != "Untitled" {
		t.Errorf("Expected default title 'Untitled', got '%s'", doc.Title)
	}
}

func TestParseHeadings(t *testing.T) {
	content := []byte(`# H1 Title

## H2 Heading

### H3 Heading`)

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("Failed to parse markdown: %v", err)
	}

	if doc.Title != "H1 Title" {
		t.Errorf("Expected title 'H1 Title', got '%s'", doc.Title)
	}

	// Check for different heading types
	foundH2 := false
	foundH3 := false

	for _, block := range doc.Blocks {
		switch block.(type) {
		case *notionapi.Heading2Block:
			foundH2 = true
		case *notionapi.Heading3Block:
			foundH3 = true
		}
	}

	if !foundH2 {
		t.Error("Expected to find H2 heading")
	}

	if !foundH3 {
		t.Error("Expected to find H3 heading")
	}
}

func TestParseCodeBlockWithoutLanguage(t *testing.T) {
	content := []byte("# Test\n\nCode without language:\n\n```\nsome code\nwithout language\n```")

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("Failed to parse markdown: %v", err)
	}

	// Check for code block
	foundCode := false
	for _, block := range doc.Blocks {
		if codeBlock, ok := block.(*notionapi.CodeBlock); ok {
			foundCode = true
			if codeBlock.Code.Language != "plain text" {
				t.Errorf("Expected language 'plain text', got '%s'", codeBlock.Code.Language)
			}
			break
		}
	}

	if !foundCode {
		t.Error("Expected to find code block")
	}
}
