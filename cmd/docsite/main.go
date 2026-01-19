// Command docsite generates HTML documentation from Markdown files.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tessro/fab/internal/docsite"
)

func main() {
	sourceDir := flag.String("source", "docs", "Source directory containing Markdown files")
	outputDir := flag.String("out", "site/public/docs", "Output directory for generated HTML files")
	templateFile := flag.String("template", "site/templates/docs.html", "HTML template file")
	flag.Parse()

	gen, err := docsite.NewGenerator(*sourceDir, *outputDir, *templateFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "🚌 Error initializing generator: %v\n", err)
		os.Exit(1)
	}

	if err := gen.Generate(); err != nil {
		fmt.Fprintf(os.Stderr, "🚌 Error generating docs: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("🚌 Documentation generated successfully")
}
