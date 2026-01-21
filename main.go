package main

import (
	"fmt"
	"log"
	"os"

	"github.com/flashingpumpkin/notion-cli/internal/sync"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "notion-cli",
		Usage: "Sync Markdown files to Notion pages",
		Commands: []*cli.Command{
			{
				Name:  "sync",
				Usage: "Sync a markdown file to Notion",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "file",
						Aliases:  []string{"f"},
						Usage:    "Path to the markdown file to sync",
						Required: true,
					},
					&cli.StringFlag{
						Name:     "token",
						Aliases:  []string{"t"},
						Usage:    "Notion API token",
						EnvVars:  []string{"NOTION_TOKEN"},
						Required: true,
					},
					&cli.StringFlag{
						Name:     "root",
						Aliases:  []string{"r"},
						Usage:    "Root page ID under which to sync the markdown page",
						Required: true,
					},
					&cli.StringFlag{
						Name:    "database",
						Aliases: []string{"d"},
						Usage:   "Database ID to store page mappings (optional, uses file-based mapping if not provided)",
					},
				},
				Action: func(c *cli.Context) error {
					config := sync.Config{
						FilePath:   c.String("file"),
						NotionToken: c.String("token"),
						RootPageID: c.String("root"),
						DatabaseID: c.String("database"),
					}

					syncer, err := sync.NewSyncer(config)
					if err != nil {
						return fmt.Errorf("failed to create syncer: %w", err)
					}

					pageID, created, err := syncer.Sync()
					if err != nil {
						return fmt.Errorf("sync failed: %w", err)
					}

					if created {
						fmt.Printf("✓ Created new Notion page: %s\n", pageID)
					} else {
						fmt.Printf("✓ Updated existing Notion page: %s\n", pageID)
					}

					return nil
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
