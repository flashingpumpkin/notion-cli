package main

import (
"fmt"
"github.com/flashingpumpkin/notion-cli/internal/markdown"
)

func main() {
content := []byte(`# List Test

## Unordered List

- Item 1
- Item 2
- Item 3

## Ordered List

1. First
2. Second
3. Third`)

doc, err := markdown.Parse(content)
if err != nil {
fmt.Printf("Error: %v\n", err)
return
}

fmt.Printf("Title: %s\n", doc.Title)
fmt.Printf("Number of blocks: %d\n", len(doc.Blocks))
for i, block := range doc.Blocks {
fmt.Printf("Block %d: %T\n", i, block)
}
}
