package notion

import (
	"testing"

	"github.com/jomei/notionapi"
)

// Mock block creation helpers

func mockParagraph(text string) *notionapi.ParagraphBlock {
	return &notionapi.ParagraphBlock{
		BasicBlock: notionapi.BasicBlock{
			Object: notionapi.ObjectTypeBlock,
			Type:   notionapi.BlockTypeParagraph,
		},
		Paragraph: notionapi.Paragraph{
			RichText: []notionapi.RichText{
				{
					Type: notionapi.ObjectTypeText,
					Text: &notionapi.Text{Content: text},
				},
			},
		},
	}
}

func mockParagraphWithAnnotations(text string, bold, italic bool) *notionapi.ParagraphBlock {
	return &notionapi.ParagraphBlock{
		BasicBlock: notionapi.BasicBlock{
			Object: notionapi.ObjectTypeBlock,
			Type:   notionapi.BlockTypeParagraph,
		},
		Paragraph: notionapi.Paragraph{
			RichText: []notionapi.RichText{
				{
					Type:        notionapi.ObjectTypeText,
					Text:        &notionapi.Text{Content: text},
					Annotations: &notionapi.Annotations{Bold: bold, Italic: italic},
				},
			},
		},
	}
}

func mockHeading2(text string) *notionapi.Heading2Block {
	return &notionapi.Heading2Block{
		BasicBlock: notionapi.BasicBlock{
			Object: notionapi.ObjectTypeBlock,
			Type:   notionapi.BlockTypeHeading2,
		},
		Heading2: notionapi.Heading{
			RichText: []notionapi.RichText{
				{
					Type: notionapi.ObjectTypeText,
					Text: &notionapi.Text{Content: text},
				},
			},
		},
	}
}

func mockCode(text, lang string) *notionapi.CodeBlock {
	return &notionapi.CodeBlock{
		BasicBlock: notionapi.BasicBlock{
			Object: notionapi.ObjectTypeBlock,
			Type:   notionapi.BlockTypeCode,
		},
		Code: notionapi.Code{
			RichText: []notionapi.RichText{
				{
					Type: notionapi.ObjectTypeText,
					Text: &notionapi.Text{Content: text},
				},
			},
			Language: lang,
		},
	}
}

func mockDivider() *notionapi.DividerBlock {
	return &notionapi.DividerBlock{
		BasicBlock: notionapi.BasicBlock{
			Object: notionapi.ObjectTypeBlock,
			Type:   notionapi.BlockTypeDivider,
		},
	}
}

func mockExistingParagraph(id, text string) *notionapi.ParagraphBlock {
	p := mockParagraph(text)
	p.BasicBlock.ID = notionapi.BlockID(id)
	return p
}

func mockExistingHeading2(id, text string) *notionapi.Heading2Block {
	h := mockHeading2(text)
	h.BasicBlock.ID = notionapi.BlockID(id)
	return h
}

func mockExistingCode(id, text, lang string) *notionapi.CodeBlock {
	c := mockCode(text, lang)
	c.BasicBlock.ID = notionapi.BlockID(id)
	return c
}

func mockExistingDivider(id string) *notionapi.DividerBlock {
	d := mockDivider()
	d.BasicBlock.ID = notionapi.BlockID(id)
	return d
}

func mockExistingParagraphWithAnnotations(id, text string, bold, italic bool) *notionapi.ParagraphBlock {
	p := mockParagraphWithAnnotations(text, bold, italic)
	p.BasicBlock.ID = notionapi.BlockID(id)
	return p
}

// countOperations counts operations by type
func countOperations(ops []Operation) (unchanged, updated, deleted, inserted, appended int) {
	for _, op := range ops {
		switch op.Type {
		case OpUnchanged:
			unchanged++
		case OpUpdate:
			updated++
		case OpDelete:
			deleted++
		case OpInsert:
			inserted += len(op.Blocks)
		case OpAppend:
			appended += len(op.Blocks)
		}
	}
	return
}

func TestBlocksAreEqual(t *testing.T) {
	tests := []struct {
		name     string
		existing notionapi.Block
		new      notionapi.Block
		want     bool
	}{
		{
			name:     "identical paragraphs",
			existing: mockParagraph("hello"),
			new:      mockParagraph("hello"),
			want:     true,
		},
		{
			name:     "different paragraph text",
			existing: mockParagraph("hello"),
			new:      mockParagraph("world"),
			want:     false,
		},
		{
			name:     "different block types",
			existing: mockParagraph("hello"),
			new:      mockHeading2("hello"),
			want:     false,
		},
		{
			name:     "identical headings",
			existing: mockHeading2("title"),
			new:      mockHeading2("title"),
			want:     true,
		},
		{
			name:     "identical code blocks",
			existing: mockCode("print(1)", "python"),
			new:      mockCode("print(1)", "python"),
			want:     true,
		},
		{
			name:     "code blocks different language",
			existing: mockCode("print(1)", "python"),
			new:      mockCode("print(1)", "javascript"),
			want:     false,
		},
		{
			name:     "identical dividers",
			existing: mockDivider(),
			new:      mockDivider(),
			want:     true,
		},
		{
			name:     "paragraph with different annotations",
			existing: mockParagraphWithAnnotations("hello", false, false),
			new:      mockParagraphWithAnnotations("hello", true, false),
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := blocksAreEqual(tt.existing, tt.new)
			if got != tt.want {
				t.Errorf("blocksAreEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindBlockInRange(t *testing.T) {
	blocks := []notionapi.Block{
		mockParagraph("a"),
		mockParagraph("b"),
		mockParagraph("c"),
		mockParagraph("d"),
		mockParagraph("e"),
	}

	tests := []struct {
		name   string
		needle notionapi.Block
		start  int
		end    int
		want   int // offset from start, or -1 if not found
	}{
		{
			name:   "find at start of range",
			needle: mockParagraph("a"),
			start:  0,
			end:    5,
			want:   0, // found at blocks[0], offset = 0-0 = 0
		},
		{
			name:   "find in middle of full range",
			needle: mockParagraph("c"),
			start:  0,
			end:    5,
			want:   2, // found at blocks[2], offset = 2-0 = 2
		},
		{
			name:   "not found",
			needle: mockParagraph("x"),
			start:  0,
			end:    5,
			want:   -1,
		},
		{
			name:   "find with offset start",
			needle: mockParagraph("c"),
			start:  2,
			end:    5,
			want:   0, // found at blocks[2], offset = 2-2 = 0
		},
		{
			name:   "not in range because before start",
			needle: mockParagraph("a"),
			start:  1,
			end:    5,
			want:   -1, // blocks[0] = "a" but 0 < start=1
		},
		{
			name:   "find second occurrence in range",
			needle: mockParagraph("d"),
			start:  1,
			end:    5,
			want:   2, // found at blocks[3], offset = 3-1 = 2
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findBlockInRange(tt.needle, blocks, tt.start, tt.end)
			if got != tt.want {
				t.Errorf("findBlockInRange() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComputeBlockDiff(t *testing.T) {
	tests := []struct {
		name         string
		existing     []notionapi.Block
		new          []notionapi.Block
		wantUnchange int
		wantUpdate   int
		wantDelete   int
		wantInsert   int
		wantAppend   int
	}{
		{
			name: "Test 1: identical blocks - no changes",
			existing: []notionapi.Block{
				mockExistingParagraph("1", "hello"),
				mockExistingParagraph("2", "world"),
			},
			new: []notionapi.Block{
				mockParagraph("hello"),
				mockParagraph("world"),
			},
			wantUnchange: 2,
			wantUpdate:   0,
			wantDelete:   0,
			wantInsert:   0,
			wantAppend:   0,
		},
		{
			name: "Test 2: all blocks different - full replacement",
			existing: []notionapi.Block{
				mockExistingParagraph("1", "a"),
				mockExistingParagraph("2", "b"),
				mockExistingParagraph("3", "c"),
			},
			new: []notionapi.Block{
				mockParagraph("x"),
				mockParagraph("y"),
				mockParagraph("z"),
			},
			wantUnchange: 0,
			wantUpdate:   3,
			wantDelete:   0,
			wantInsert:   0,
			wantAppend:   0,
		},
		{
			name: "Test 3: single block updated in middle",
			existing: []notionapi.Block{
				mockExistingParagraph("1", "a"),
				mockExistingParagraph("2", "b"),
				mockExistingParagraph("3", "c"),
			},
			new: []notionapi.Block{
				mockParagraph("a"),
				mockParagraph("B"),
				mockParagraph("c"),
			},
			wantUnchange: 2,
			wantUpdate:   1,
			wantDelete:   0,
			wantInsert:   0,
			wantAppend:   0,
		},
		{
			name: "Test 4: single block appended at end",
			existing: []notionapi.Block{
				mockExistingParagraph("1", "a"),
				mockExistingParagraph("2", "b"),
			},
			new: []notionapi.Block{
				mockParagraph("a"),
				mockParagraph("b"),
				mockParagraph("c"),
			},
			wantUnchange: 2,
			wantUpdate:   0,
			wantDelete:   0,
			wantInsert:   0,
			wantAppend:   1,
		},
		{
			name: "Test 5: single block deleted from end",
			existing: []notionapi.Block{
				mockExistingParagraph("1", "a"),
				mockExistingParagraph("2", "b"),
				mockExistingParagraph("3", "c"),
			},
			new: []notionapi.Block{
				mockParagraph("a"),
				mockParagraph("b"),
			},
			wantUnchange: 2,
			wantUpdate:   0,
			wantDelete:   1,
			wantInsert:   0,
			wantAppend:   0,
		},
		{
			name: "Test 6: single block inserted at beginning - fallback",
			existing: []notionapi.Block{
				mockExistingParagraph("1", "a"),
				mockExistingParagraph("2", "b"),
			},
			new: []notionapi.Block{
				mockParagraph("x"),
				mockParagraph("a"),
				mockParagraph("b"),
			},
			// Falls back to delete-all and append-all since we can't insert before first
			wantUnchange: 0,
			wantUpdate:   0,
			wantDelete:   2,
			wantInsert:   0,
			wantAppend:   3,
		},
		{
			name: "Test 7: single block inserted in middle - falls back to delete+append",
			existing: []notionapi.Block{
				mockExistingParagraph("1", "a"),
				mockExistingParagraph("2", "c"),
			},
			new: []notionapi.Block{
				mockParagraph("a"),
				mockParagraph("b"),
				mockParagraph("c"),
			},
			// Falls back to delete remaining (c) and append remaining (b, c)
			wantUnchange: 1,
			wantUpdate:   0,
			wantDelete:   1,
			wantInsert:   0,
			wantAppend:   2,
		},
		{
			name: "Test 8: single block deleted from middle",
			existing: []notionapi.Block{
				mockExistingParagraph("1", "a"),
				mockExistingParagraph("2", "b"),
				mockExistingParagraph("3", "c"),
			},
			new: []notionapi.Block{
				mockParagraph("a"),
				mockParagraph("c"),
			},
			wantUnchange: 2,
			wantUpdate:   0,
			wantDelete:   1,
			wantInsert:   0,
			wantAppend:   0,
		},
		{
			name: "Test 9: single block deleted from beginning",
			existing: []notionapi.Block{
				mockExistingParagraph("1", "a"),
				mockExistingParagraph("2", "b"),
				mockExistingParagraph("3", "c"),
			},
			new: []notionapi.Block{
				mockParagraph("b"),
				mockParagraph("c"),
			},
			wantUnchange: 2,
			wantUpdate:   0,
			wantDelete:   1,
			wantInsert:   0,
			wantAppend:   0,
		},
		{
			name: "Test 10: multiple blocks inserted in middle - falls back to delete+append",
			existing: []notionapi.Block{
				mockExistingParagraph("1", "a"),
				mockExistingParagraph("2", "d"),
			},
			new: []notionapi.Block{
				mockParagraph("a"),
				mockParagraph("b"),
				mockParagraph("c"),
				mockParagraph("d"),
			},
			// Falls back to delete remaining (d) and append remaining (b, c, d)
			wantUnchange: 1,
			wantUpdate:   0,
			wantDelete:   1,
			wantInsert:   0,
			wantAppend:   3,
		},
		{
			name: "Test 11: multiple blocks deleted from middle",
			existing: []notionapi.Block{
				mockExistingParagraph("1", "a"),
				mockExistingParagraph("2", "b"),
				mockExistingParagraph("3", "c"),
				mockExistingParagraph("4", "d"),
			},
			new: []notionapi.Block{
				mockParagraph("a"),
				mockParagraph("d"),
			},
			wantUnchange: 2,
			wantUpdate:   0,
			wantDelete:   2,
			wantInsert:   0,
			wantAppend:   0,
		},
		{
			name: "Test 12: block type changed (paragraph to heading) - falls back to delete+append",
			existing: []notionapi.Block{
				mockExistingParagraph("1", "a"),
				mockExistingParagraph("2", "b"),
				mockExistingParagraph("3", "c"),
			},
			new: []notionapi.Block{
				mockParagraph("a"),
				mockHeading2("b"),
				mockParagraph("c"),
			},
			// Falls back to delete remaining (b, c) and append remaining (H2("b"), P("c"))
			wantUnchange: 1,
			wantUpdate:   0,
			wantDelete:   2,
			wantInsert:   0,
			wantAppend:   2,
		},
		{
			name:     "Test 13: empty old list",
			existing: []notionapi.Block{},
			new: []notionapi.Block{
				mockParagraph("a"),
				mockParagraph("b"),
			},
			wantUnchange: 0,
			wantUpdate:   0,
			wantDelete:   0,
			wantInsert:   0,
			wantAppend:   2,
		},
		{
			name: "Test 14: empty new list",
			existing: []notionapi.Block{
				mockExistingParagraph("1", "a"),
				mockExistingParagraph("2", "b"),
			},
			new:          []notionapi.Block{},
			wantUnchange: 0,
			wantUpdate:   0,
			wantDelete:   2,
			wantInsert:   0,
			wantAppend:   0,
		},
		{
			name: "Test 15: complex scenario - mixed operations",
			existing: []notionapi.Block{
				mockExistingParagraph("1", "a"),
				mockExistingParagraph("2", "b"),
				mockExistingParagraph("3", "c"),
				mockExistingParagraph("4", "d"),
			},
			new: []notionapi.Block{
				mockParagraph("a"),
				mockParagraph("x"),
				mockParagraph("c"),
				mockParagraph("e"),
			},
			// a=a (unchanged), b!=x (b found at none, x found at none -> update)
			// c=c (unchanged), d!=e (update)
			wantUnchange: 2,
			wantUpdate:   2,
			wantDelete:   0,
			wantInsert:   0,
			wantAppend:   0,
		},
		{
			name: "Test 16: blocks with different formatting (bold vs plain)",
			existing: []notionapi.Block{
				mockExistingParagraphWithAnnotations("1", "hello", false, false),
			},
			new: []notionapi.Block{
				mockParagraphWithAnnotations("hello", true, false),
			},
			wantUnchange: 0,
			wantUpdate:   1,
			wantDelete:   0,
			wantInsert:   0,
			wantAppend:   0,
		},
		{
			name: "Test 17: code block language change",
			existing: []notionapi.Block{
				mockExistingCode("1", "print(1)", "python"),
			},
			new: []notionapi.Block{
				mockCode("print(1)", "javascript"),
			},
			// Same type but different language - update
			wantUnchange: 0,
			wantUpdate:   1,
			wantDelete:   0,
			wantInsert:   0,
			wantAppend:   0,
		},
		{
			name: "Test 18: dividers are always equal",
			existing: []notionapi.Block{
				mockExistingDivider("1"),
			},
			new: []notionapi.Block{
				mockDivider(),
			},
			wantUnchange: 1,
			wantUpdate:   0,
			wantDelete:   0,
			wantInsert:   0,
			wantAppend:   0,
		},
		{
			name: "Test 19: lookahead finds match - falls back to delete+append",
			existing: []notionapi.Block{
				mockExistingParagraph("1", "a"),
				mockExistingParagraph("2", "b"),
				mockExistingParagraph("3", "c"),
				mockExistingParagraph("4", "d"),
				mockExistingParagraph("5", "e"),
			},
			new: []notionapi.Block{
				mockParagraph("a"),
				mockParagraph("x"),
				mockParagraph("y"),
				mockParagraph("b"),
				mockParagraph("c"),
				mockParagraph("d"),
				mockParagraph("e"),
			},
			// a=a (unchanged)
			// b!=x, b found at new[3] -> falls back to delete remaining (b,c,d,e) and append remaining (x,y,b,c,d,e)
			wantUnchange: 1,
			wantUpdate:   0,
			wantDelete:   4,
			wantInsert:   0,
			wantAppend:   6,
		},
		{
			name: "Test 20: multiple updates in sequence",
			existing: []notionapi.Block{
				mockExistingParagraph("1", "a"),
				mockExistingParagraph("2", "b"),
				mockExistingParagraph("3", "c"),
			},
			new: []notionapi.Block{
				mockParagraph("A"),
				mockParagraph("B"),
				mockParagraph("C"),
			},
			wantUnchange: 0,
			wantUpdate:   3,
			wantDelete:   0,
			wantInsert:   0,
			wantAppend:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := computeBlockDiff(tt.existing, tt.new)

			if result.Unchanged != tt.wantUnchange {
				t.Errorf("Unchanged = %d, want %d", result.Unchanged, tt.wantUnchange)
			}
			if result.Updated != tt.wantUpdate {
				t.Errorf("Updated = %d, want %d", result.Updated, tt.wantUpdate)
			}
			if result.Deleted != tt.wantDelete {
				t.Errorf("Deleted = %d, want %d", result.Deleted, tt.wantDelete)
			}
			if result.Inserted != tt.wantInsert {
				t.Errorf("Inserted = %d, want %d", result.Inserted, tt.wantInsert)
			}
			if result.Appended != tt.wantAppend {
				t.Errorf("Appended = %d, want %d", result.Appended, tt.wantAppend)
			}

			// Also verify via countOperations to double-check consistency
			gotUnchanged, gotUpdated, gotDeleted, gotInserted, gotAppended := countOperations(result.Operations)
			if gotUnchanged != result.Unchanged {
				t.Errorf("countOperations Unchanged mismatch: got %d, result has %d", gotUnchanged, result.Unchanged)
			}
			if gotUpdated != result.Updated {
				t.Errorf("countOperations Updated mismatch: got %d, result has %d", gotUpdated, result.Updated)
			}
			if gotDeleted != result.Deleted {
				t.Errorf("countOperations Deleted mismatch: got %d, result has %d", gotDeleted, result.Deleted)
			}
			if gotInserted != result.Inserted {
				t.Errorf("countOperations Inserted mismatch: got %d, result has %d", gotInserted, result.Inserted)
			}
			if gotAppended != result.Appended {
				t.Errorf("countOperations Appended mismatch: got %d, result has %d", gotAppended, result.Appended)
			}
		})
	}
}

func TestComputeBlockDiff_OperationDetails(t *testing.T) {
	t.Run("insert scenario falls back to delete+append", func(t *testing.T) {
		existing := []notionapi.Block{
			mockExistingParagraph("block-1", "a"),
			mockExistingParagraph("block-2", "c"),
		}
		new := []notionapi.Block{
			mockParagraph("a"),
			mockParagraph("b"),
			mockParagraph("c"),
		}

		result := computeBlockDiff(existing, new)

		// Should have 1 unchanged (a), 1 delete (c), 1 append (b, c)
		if result.Unchanged != 1 {
			t.Errorf("Unchanged = %d, want 1", result.Unchanged)
		}
		if result.Deleted != 1 {
			t.Errorf("Deleted = %d, want 1", result.Deleted)
		}
		if result.Appended != 2 {
			t.Errorf("Appended = %d, want 2", result.Appended)
		}

		// Find the delete operation and verify it deletes block-2
		var deleteOp *Operation
		for i := range result.Operations {
			if result.Operations[i].Type == OpDelete {
				deleteOp = &result.Operations[i]
				break
			}
		}

		if deleteOp == nil {
			t.Fatal("expected delete operation")
		}

		if deleteOp.BlockID != "block-2" {
			t.Errorf("delete BlockID = %s, want block-2", deleteOp.BlockID)
		}
	})

	t.Run("delete operation has correct blockID", func(t *testing.T) {
		existing := []notionapi.Block{
			mockExistingParagraph("block-1", "a"),
			mockExistingParagraph("block-2", "b"),
			mockExistingParagraph("block-3", "c"),
		}
		new := []notionapi.Block{
			mockParagraph("a"),
			mockParagraph("c"),
		}

		result := computeBlockDiff(existing, new)

		// Find the delete operation
		var deleteOp *Operation
		for i := range result.Operations {
			if result.Operations[i].Type == OpDelete {
				deleteOp = &result.Operations[i]
				break
			}
		}

		if deleteOp == nil {
			t.Fatal("expected delete operation")
		}

		if deleteOp.BlockID != "block-2" {
			t.Errorf("delete BlockID = %s, want block-2", deleteOp.BlockID)
		}
	})

	t.Run("update operation has correct blockID and new content", func(t *testing.T) {
		existing := []notionapi.Block{
			mockExistingParagraph("block-1", "old"),
		}
		new := []notionapi.Block{
			mockParagraph("new"),
		}

		result := computeBlockDiff(existing, new)

		// Find the update operation
		var updateOp *Operation
		for i := range result.Operations {
			if result.Operations[i].Type == OpUpdate {
				updateOp = &result.Operations[i]
				break
			}
		}

		if updateOp == nil {
			t.Fatal("expected update operation")
		}

		if updateOp.BlockID != "block-1" {
			t.Errorf("update BlockID = %s, want block-1", updateOp.BlockID)
		}

		if len(updateOp.Blocks) != 1 {
			t.Errorf("update Blocks len = %d, want 1", len(updateOp.Blocks))
		}
	})
}

func TestRichTextEqual(t *testing.T) {
	tests := []struct {
		name string
		a    []notionapi.RichText
		b    []notionapi.RichText
		want bool
	}{
		{
			name: "identical simple text",
			a: []notionapi.RichText{
				{Text: &notionapi.Text{Content: "hello"}},
			},
			b: []notionapi.RichText{
				{Text: &notionapi.Text{Content: "hello"}},
			},
			want: true,
		},
		{
			name: "different text",
			a: []notionapi.RichText{
				{Text: &notionapi.Text{Content: "hello"}},
			},
			b: []notionapi.RichText{
				{Text: &notionapi.Text{Content: "world"}},
			},
			want: false,
		},
		{
			name: "different lengths",
			a: []notionapi.RichText{
				{Text: &notionapi.Text{Content: "hello"}},
			},
			b: []notionapi.RichText{
				{Text: &notionapi.Text{Content: "hello"}},
				{Text: &notionapi.Text{Content: "world"}},
			},
			want: false,
		},
		{
			name: "same text with link vs without",
			a: []notionapi.RichText{
				{Text: &notionapi.Text{Content: "hello", Link: &notionapi.Link{Url: "http://example.com"}}},
			},
			b: []notionapi.RichText{
				{Text: &notionapi.Text{Content: "hello"}},
			},
			want: false,
		},
		{
			name: "same text with different links",
			a: []notionapi.RichText{
				{Text: &notionapi.Text{Content: "hello", Link: &notionapi.Link{Url: "http://a.com"}}},
			},
			b: []notionapi.RichText{
				{Text: &notionapi.Text{Content: "hello", Link: &notionapi.Link{Url: "http://b.com"}}},
			},
			want: false,
		},
		{
			name: "same text same links",
			a: []notionapi.RichText{
				{Text: &notionapi.Text{Content: "hello", Link: &notionapi.Link{Url: "http://a.com"}}},
			},
			b: []notionapi.RichText{
				{Text: &notionapi.Text{Content: "hello", Link: &notionapi.Link{Url: "http://a.com"}}},
			},
			want: true,
		},
		{
			name: "different annotations - bold",
			a: []notionapi.RichText{
				{Text: &notionapi.Text{Content: "hello"}, Annotations: &notionapi.Annotations{Bold: true}},
			},
			b: []notionapi.RichText{
				{Text: &notionapi.Text{Content: "hello"}, Annotations: &notionapi.Annotations{Bold: false}},
			},
			want: false,
		},
		{
			name: "empty slices",
			a:    []notionapi.RichText{},
			b:    []notionapi.RichText{},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := richTextEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("richTextEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}
