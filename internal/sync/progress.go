package sync

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/mattn/go-isatty"
)

// ProgressManager handles display of sync progress with a sticky footer
type ProgressManager struct {
	isTTY       bool
	mu          sync.Mutex
	activeFiles map[string]*FileProgress
	footerLines int
}

// FileProgress tracks progress for a single file
type FileProgress struct {
	filename string
	current  int
	total    int
}

// NewProgressManager creates a new progress manager
func NewProgressManager() *ProgressManager {
	return &ProgressManager{
		isTTY:       !isCI() && isatty.IsTerminal(os.Stdout.Fd()),
		activeFiles: make(map[string]*FileProgress),
	}
}

// isCI detects if running in a CI environment
func isCI() bool {
	return os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != ""
}

// Start initialises the progress display
func (p *ProgressManager) Start() {
	// Nothing to do - we render on demand
}

// Stop cleans up the progress display
func (p *ProgressManager) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isTTY && p.footerLines > 0 {
		// Clear the footer
		p.clearFooter()
	}
}

// PrintCompleted prints a completed file status (scrolls in the main area)
func (p *ProgressManager) PrintCompleted(filename, status string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Remove from active files
	delete(p.activeFiles, filename)

	if p.isTTY {
		// Clear footer, print message, redraw footer
		p.clearFooter()
		fmt.Printf("+ %s (%s)\n", filename, status)
		p.drawFooter()
	} else {
		// Simple output for CI
		fmt.Printf("+ %s (%s)\n", filename, status)
	}
}

// PrintError prints an error for a file
func (p *ProgressManager) PrintError(filename string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Remove from active files
	delete(p.activeFiles, filename)

	if p.isTTY {
		p.clearFooter()
		fmt.Printf("x %s (error: %v)\n", filename, err)
		p.drawFooter()
	} else {
		fmt.Printf("x %s (error: %v)\n", filename, err)
	}
}

// AddActiveFile registers a file as being actively synced
func (p *ProgressManager) AddActiveFile(filename string, total int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.activeFiles[filename] = &FileProgress{
		filename: filename,
		current:  0,
		total:    total,
	}

	if p.isTTY {
		p.clearFooter()
		p.drawFooter()
	}
}

// UpdateProgress updates the progress for an active file
func (p *ProgressManager) UpdateProgress(filename string, current, total int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if fp, ok := p.activeFiles[filename]; ok {
		fp.current = current
		fp.total = total
	}

	if p.isTTY {
		p.clearFooter()
		p.drawFooter()
	}
}

// RemoveActiveFile removes a file from active tracking
func (p *ProgressManager) RemoveActiveFile(filename string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.activeFiles, filename)

	if p.isTTY {
		p.clearFooter()
		p.drawFooter()
	}
}

// ProgressCallback returns a callback function for block progress updates
func (p *ProgressManager) ProgressCallback(filename string) func(current, total int) {
	return func(current, total int) {
		p.UpdateProgress(filename, current, total)
	}
}

// clearFooter clears the footer area (must be called with lock held)
func (p *ProgressManager) clearFooter() {
	if p.footerLines > 0 {
		// Move up and clear each line
		for i := 0; i < p.footerLines; i++ {
			fmt.Print("\033[A\033[K") // Move up one line and clear it
		}
		p.footerLines = 0
	}
}

// drawFooter draws the progress footer (must be called with lock held)
func (p *ProgressManager) drawFooter() {
	if len(p.activeFiles) == 0 {
		p.footerLines = 0
		return
	}

	// Sort filenames for stable display order
	names := make([]string, 0, len(p.activeFiles))
	for name := range p.activeFiles {
		names = append(names, name)
	}
	sort.Strings(names)

	lines := 0
	for _, name := range names {
		fp := p.activeFiles[name]
		bar := renderProgressBar(fp.current, fp.total, 20)
		fmt.Printf("> %-30s %s %d/%d blocks\n", truncate(fp.filename, 30), bar, fp.current, fp.total)
		lines++
	}
	p.footerLines = lines
}

// renderProgressBar creates a text progress bar
func renderProgressBar(current, total, width int) string {
	if total == 0 {
		return "[" + strings.Repeat("-", width) + "]"
	}

	filled := (current * width) / total
	if filled > width {
		filled = width
	}

	empty := width - filled
	return "[" + strings.Repeat("=", filled) + strings.Repeat("-", empty) + "]"
}

// truncate shortens a string to max length, adding ellipsis if needed
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
