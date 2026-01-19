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

	// Rewrite internal .md links to pretty URLs
	htmlContent := RewriteLinks(htmlBuf.String())

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
		Content: template.HTML(htmlContent),
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

// mdLinkRegex matches href attributes that point to .md files.
// Captures: (1) path before .md (2) .md extension (3) optional anchor
var mdLinkRegex = regexp.MustCompile(`href="([^"]*?)(\.md)(#[^"]*)?"`)

// RewriteLinks transforms internal .md links in HTML content to pretty URLs.
// For example: href="./components/issue-backends.md#section" becomes href="./components/issue-backends/#section"
// Links to index.md are handled specially: ./foo/index.md becomes ./foo/
func RewriteLinks(html string) string {
	return mdLinkRegex.ReplaceAllStringFunc(html, func(match string) string {
		submatches := mdLinkRegex.FindStringSubmatch(match)
		if len(submatches) < 3 {
			return match
		}

		path := submatches[1]   // Path before .md
		anchor := ""            // Optional anchor
		if len(submatches) > 3 {
			anchor = submatches[3]
		}

		// Check if the path ends with /index or is just "index"
		if strings.HasSuffix(path, "/index") {
			// ./foo/index.md -> ./foo/
			path = strings.TrimSuffix(path, "index")
		} else if path == "index" {
			// index.md -> ./
			path = "./"
		} else {
			// ./foo.md -> ./foo/
			path = path + "/"
		}

		return fmt.Sprintf(`href="%s%s"`, path, anchor)
	})
}

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

// MapPath converts a source markdown path to an output HTML path using pretty URLs.
// Files named "index.md" stay as index.html, other files become directories with index.html.
// Example: docs/foo.md -> site/public/docs/foo/index.html
// Example: docs/index.md -> site/public/docs/index.html
func MapPath(sourceDir, outputDir, inputPath string) string {
	// Get the relative path from source directory
	relPath, err := filepath.Rel(sourceDir, inputPath)
	if err != nil {
		// Fallback: use the filename
		relPath = filepath.Base(inputPath)
	}

	// Remove .md extension
	relPath = strings.TrimSuffix(relPath, ".md")

	// Get the base filename
	base := filepath.Base(relPath)

	// If it's already "index", just add .html extension
	if base == "index" {
		return filepath.Join(outputDir, relPath+".html")
	}

	// Otherwise, create a directory with the same name and put index.html inside
	return filepath.Join(outputDir, relPath, "index.html")
}
