package main

import (
	"fmt"
	"log"
	"os"

	"github.com/flashingpumpkin/notion-cli/internal/sync"
	"github.com/urfave/cli/v2"
)

var version = "dev"

func main() {
	app := &cli.App{
		Name:    "notion-cli",
		Usage:   "Sync Markdown files to Notion pages",
		Version: version,
		Commands: []*cli.Command{
			{
				Name:  "sync",
				Usage: "Sync a markdown file or directory to Notion",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "file",
						Aliases:  []string{"f"},
						Usage:    "Path to the markdown file or directory to sync",
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
						Usage:    "Notion page ID (for single file) or database ID (for directory) to sync into",
						Required: true,
					},
					&cli.BoolFlag{
						Name:    "cleanup",
						Aliases: []string{"c"},
						Usage:   "Delete database entries without corresponding markdown files (directory sync only)",
						Value:   false,
					},
				},
				Action: func(c *cli.Context) error {
					config := sync.Config{
						FilePath:    c.String("file"),
						NotionToken: c.String("token"),
						RootPageID:  c.String("root"),
					}

					syncer, err := sync.NewSyncer(config)
					if err != nil {
						return fmt.Errorf("failed to create syncer: %w", err)
					}

					pageID, created, err := syncer.Sync()
					if err != nil {
						return fmt.Errorf("sync failed: %w", err)
					}

					// Only print page ID for single file sync
					if pageID != "" {
						if created {
							fmt.Printf("✓ Created new Notion page: %s\n", pageID)
						} else {
							fmt.Printf("✓ Updated existing Notion page: %s\n", pageID)
						}
					} else {
						fmt.Println("✓ Directory sync completed")
					}

					// If cleanup flag is set, clean up orphaned pages
					if c.Bool("cleanup") {
						fmt.Println("\nCleaning up orphaned pages...")
						err = syncer.CleanupOrphanedPages([]string{c.String("file")})
						if err != nil {
							return fmt.Errorf("cleanup failed: %w", err)
						}
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
