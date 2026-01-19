package docsite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMapPath(t *testing.T) {
	tests := []struct {
		name      string
		sourceDir string
		outputDir string
		inputPath string
		want      string
	}{
		{
			name:      "simple file becomes directory",
			sourceDir: "docs",
			outputDir: "site/public/docs",
			inputPath: "docs/foo.md",
			want:      "site/public/docs/foo/index.html",
		},
		{
			name:      "nested file becomes directory",
			sourceDir: "docs",
			outputDir: "site/public/docs",
			inputPath: "docs/components/bar.md",
			want:      "site/public/docs/components/bar/index.html",
		},
		{
			name:      "index file stays as index.html",
			sourceDir: "docs",
			outputDir: "site/public/docs",
			inputPath: "docs/index.md",
			want:      "site/public/docs/index.html",
		},
		{
			name:      "deeply nested becomes directory",
			sourceDir: "docs",
			outputDir: "out",
			inputPath: "docs/a/b/c.md",
			want:      "out/a/b/c/index.html",
		},
		{
			name:      "nested index stays as index.html",
			sourceDir: "docs",
			outputDir: "site/public/docs",
			inputPath: "docs/components/index.md",
			want:      "site/public/docs/components/index.html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapPath(tt.sourceDir, tt.outputDir, tt.inputPath)
			// Normalize paths for cross-platform comparison
			got = filepath.ToSlash(got)
			want := filepath.ToSlash(tt.want)
			if got != want {
				t.Errorf("MapPath() = %q, want %q", got, want)
			}
		})
	}
}

func TestExtractTitle(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		filePath string
		want     string
	}{
		{
			name:     "h1 at start",
			content:  "# My Title\n\nSome content here.",
			filePath: "docs/test.md",
			want:     "My Title",
		},
		{
			name:     "h1 with leading content",
			content:  "Some intro text\n\n# The Real Title\n\nMore content.",
			filePath: "docs/test.md",
			want:     "The Real Title",
		},
		{
			name:     "no h1 falls back to filename",
			content:  "## Only H2\n\nNo main title here.",
			filePath: "docs/my-document.md",
			want:     "my-document",
		},
		{
			name:     "h1 with extra spaces",
			content:  "#   Spaced Title   \n\nContent.",
			filePath: "docs/test.md",
			want:     "Spaced Title",
		},
		{
			name:     "nested path filename fallback",
			content:  "No title",
			filePath: "docs/components/issue-backends.md",
			want:     "issue-backends",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractTitle([]byte(tt.content), tt.filePath)
			if got != tt.want {
				t.Errorf("ExtractTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRewriteLinks(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple md link",
			input: `<a href="./foo.md">Foo</a>`,
			want:  `<a href="./foo/">Foo</a>`,
		},
		{
			name:  "md link with anchor",
			input: `<a href="./components/issue-backends.md#section">Issue Backends</a>`,
			want:  `<a href="./components/issue-backends/#section">Issue Backends</a>`,
		},
		{
			name:  "index.md link",
			input: `<a href="index.md">Home</a>`,
			want:  `<a href="./">Home</a>`,
		},
		{
			name:  "nested index.md link",
			input: `<a href="./components/index.md">Components</a>`,
			want:  `<a href="./components/">Components</a>`,
		},
		{
			name:  "index.md with anchor",
			input: `<a href="index.md#intro">Intro</a>`,
			want:  `<a href="./#intro">Intro</a>`,
		},
		{
			name:  "non-md link unchanged",
			input: `<a href="https://example.com">External</a>`,
			want:  `<a href="https://example.com">External</a>`,
		},
		{
			name:  "html link unchanged",
			input: `<a href="./foo.html">Foo</a>`,
			want:  `<a href="./foo.html">Foo</a>`,
		},
		{
			name:  "multiple links",
			input: `<a href="./a.md">A</a> and <a href="./b.md#x">B</a>`,
			want:  `<a href="./a/">A</a> and <a href="./b/#x">B</a>`,
		},
		{
			name:  "relative link without dot",
			input: `<a href="components/bar.md">Bar</a>`,
			want:  `<a href="components/bar/">Bar</a>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RewriteLinks(tt.input)
			if got != tt.want {
				t.Errorf("RewriteLinks() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGeneratorGolden(t *testing.T) {
	// Create a temporary directory for our test
	tmpDir := t.TempDir()

	// Create source directory with a test markdown file
	sourceDir := filepath.Join(tmpDir, "docs")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("creating source dir: %v", err)
	}

	mdContent := `# Test Page

This is a test document.

See also [Other Page](./other.md) and [Section](#section).

| Column A | Column B |
|----------|----------|
| Value 1  | Value 2  |
`
	if err := os.WriteFile(filepath.Join(sourceDir, "test.md"), []byte(mdContent), 0o644); err != nil {
		t.Fatalf("writing test markdown: %v", err)
	}

	// Create template directory and file
	templateDir := filepath.Join(tmpDir, "templates")
	if err := os.MkdirAll(templateDir, 0o755); err != nil {
		t.Fatalf("creating template dir: %v", err)
	}

	templateContent := `<!DOCTYPE html>
<html>
<head><title>{{.Title}} | fab</title><link rel="stylesheet" href="/style.css"></head>
<body>
<header><nav><a href="/" class="logo">fab</a></nav></header>
<main>{{.Content}}</main>
<footer><p>made with love</p></footer>
</body>
</html>`
	templateFile := filepath.Join(templateDir, "docs.html")
	if err := os.WriteFile(templateFile, []byte(templateContent), 0o644); err != nil {
		t.Fatalf("writing template: %v", err)
	}

	// Create output directory
	outputDir := filepath.Join(tmpDir, "site", "public", "docs")

	// Run the generator
	gen, err := NewGenerator(sourceDir, outputDir, templateFile)
	if err != nil {
		t.Fatalf("creating generator: %v", err)
	}

	if err := gen.Generate(); err != nil {
		t.Fatalf("generating docs: %v", err)
	}

	// Check the output file exists (pretty URL: test/index.html)
	outputFile := filepath.Join(outputDir, "test", "index.html")
	output, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}

	outputStr := string(output)

	// Verify the output contains expected elements
	checks := []struct {
		name     string
		contains string
	}{
		{"title", "<title>Test Page | fab</title>"},
		{"stylesheet link", `href="/style.css"`},
		{"header", "<header>"},
		{"nav logo", `class="logo">`},
		{"footer", "<footer>"},
		{"content heading", "<h1"},
		{"table element", "<table>"},
		{"rewritten link", `href="./other/"`},
		{"anchor link preserved", `href="#section"`},
	}

	for _, check := range checks {
		if !strings.Contains(outputStr, check.contains) {
			t.Errorf("output missing %s: expected to contain %q", check.name, check.contains)
		}
	}

	// Verify .md link was rewritten (should NOT contain .md in href)
	if strings.Contains(outputStr, `href="./other.md"`) {
		t.Error("output contains unrewritten .md link")
	}
}
