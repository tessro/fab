// Package docsite provides functionality to generate HTML documentation from Markdown files.
package docsite

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

// Generator generates HTML documentation from Markdown files.
type Generator struct {
	SourceDir    string
	OutputDir    string
	TemplateFile string
	md           goldmark.Markdown
	tmpl         *template.Template
}

// PageData holds data passed to the HTML template.
type PageData struct {
	Title   string
	Content template.HTML
}

// NewGenerator creates a new documentation generator.
func NewGenerator(sourceDir, outputDir, templateFile string) (*Generator, error) {
	g := &Generator{
		SourceDir:    sourceDir,
		OutputDir:    outputDir,
		TemplateFile: templateFile,
	}

	// Configure goldmark with tables and autolinks
	g.md = goldmark.New(
		goldmark.WithExtensions(
			extension.Table,
			extension.Linkify,
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			html.WithUnsafe(),
		),
	)

	// Load the template
	tmplContent, err := os.ReadFile(templateFile)
	if err != nil {
		return nil, fmt.Errorf("reading template: %w", err)
	}

	g.tmpl, err = template.New("docs").Parse(string(tmplContent))
	if err != nil {
		return nil, fmt.Errorf("parsing template: %w", err)
	}

	return g, nil
}

// Generate walks the source directory and generates HTML files in the output directory.
func (g *Generator) Generate() error {
	// Ensure output directory exists
	if err := os.MkdirAll(g.OutputDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	return filepath.WalkDir(g.SourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Only process .md files
		if !strings.HasSuffix(path, ".md") {
			return nil
		}

		return g.processFile(path)
	})
}

// processFile converts a single Markdown file to HTML.
func (g *Generator) processFile(inputPath string) error {
	// Read the markdown content
	content, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", inputPath, err)
	}

	// Convert markdown to HTML
	var htmlBuf bytes.Buffer
	if err := g.md.Convert(content, &htmlBuf); err != nil {
		return fmt.Errorf("converting %s: %w", inputPath, err)
	}

	// Extract title
	title := ExtractTitle(content, inputPath)

	// Calculate output path
	outputPath := MapPath(g.SourceDir, g.OutputDir, inputPath)

	// Ensure the output directory exists
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("creating directory for %s: %w", outputPath, err)
	}

	// Render the template
	data := PageData{
		Title:   title,
		Content: template.HTML(htmlBuf.String()),
	}

	var outBuf bytes.Buffer
	if err := g.tmpl.Execute(&outBuf, data); err != nil {
		return fmt.Errorf("executing template for %s: %w", inputPath, err)
	}

	// Write the output file
	if err := os.WriteFile(outputPath, outBuf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", outputPath, err)
	}

	return nil
}

var h1Regex = regexp.MustCompile(`(?m)^#\s+(.+)$`)

// ExtractTitle extracts the title from markdown content.
// It looks for the first H1 heading and falls back to the filename.
func ExtractTitle(content []byte, filePath string) string {
	matches := h1Regex.FindSubmatch(content)
	if len(matches) > 1 {
		return strings.TrimSpace(string(matches[1]))
	}

	// Fallback to filename without extension
	base := filepath.Base(filePath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// MapPath converts a source markdown path to an output HTML path.
// Example: docs/foo.md -> site/public/docs/foo.html
func MapPath(sourceDir, outputDir, inputPath string) string {
	// Get the relative path from source directory
	relPath, err := filepath.Rel(sourceDir, inputPath)
	if err != nil {
		// Fallback: use the filename
		relPath = filepath.Base(inputPath)
	}

	// Change extension from .md to .html
	relPath = strings.TrimSuffix(relPath, ".md") + ".html"

	return filepath.Join(outputDir, relPath)
}
