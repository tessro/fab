// Package tk implements the tk file-based issue backend.
package tk

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/tessro/fab/internal/issue"
	"gopkg.in/yaml.v3"
)

// frontmatter represents the YAML frontmatter of a tk issue file.
type frontmatter struct {
	ID       string    `yaml:"id"`
	Status   string    `yaml:"status"`
	Deps     []string  `yaml:"deps"`
	Links    []string  `yaml:"links"`
	Created  time.Time `yaml:"created"`
	Type     string    `yaml:"type"`
	Priority int       `yaml:"priority"`
	Labels   []string  `yaml:"labels,omitempty"`
}

// parseIssue parses a tk markdown file into an Issue.
func parseIssue(data []byte) (*issue.Issue, error) {
	fm, body, err := splitFrontmatter(data)
	if err != nil {
		return nil, err
	}

	var meta frontmatter
	if err := yaml.Unmarshal(fm, &meta); err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}

	title, description := parseBody(body)

	return &issue.Issue{
		ID:           meta.ID,
		Title:        title,
		Description:  description,
		Status:       issue.Status(meta.Status),
		Priority:     meta.Priority,
		Type:         meta.Type,
		Dependencies: meta.Deps,
		Labels:       meta.Labels,
		Links:        meta.Links,
		Created:      meta.Created,
	}, nil
}

// splitFrontmatter separates YAML frontmatter from markdown body.
// Expects format: ---\nyaml\n---\nbody
func splitFrontmatter(data []byte) ([]byte, []byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))

	// First line must be ---
	if !scanner.Scan() {
		return nil, nil, fmt.Errorf("empty file")
	}
	if strings.TrimSpace(scanner.Text()) != "---" {
		return nil, nil, fmt.Errorf("missing opening frontmatter delimiter")
	}

	// Collect frontmatter until closing ---
	var fmLines []string
	foundClose := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "---" {
			foundClose = true
			break
		}
		fmLines = append(fmLines, line)
	}
	if !foundClose {
		return nil, nil, fmt.Errorf("missing closing frontmatter delimiter")
	}

	// Rest is body
	var bodyLines []string
	for scanner.Scan() {
		bodyLines = append(bodyLines, scanner.Text())
	}

	fm := []byte(strings.Join(fmLines, "\n"))
	body := []byte(strings.Join(bodyLines, "\n"))

	return fm, body, scanner.Err()
}

// parseBody extracts title and description from markdown body.
// Title is the first # heading, description is everything after.
func parseBody(body []byte) (title, description string) {
	lines := strings.Split(string(body), "\n")

	// Skip leading empty lines
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}

	// Look for title (# heading)
	if start < len(lines) && strings.HasPrefix(lines[start], "# ") {
		title = strings.TrimPrefix(lines[start], "# ")
		start++
	}

	// Rest is description (trimmed)
	if start < len(lines) {
		description = strings.TrimSpace(strings.Join(lines[start:], "\n"))
	}

	return title, description
}

// formatIssue formats an Issue as tk markdown file content.
func formatIssue(iss *issue.Issue) ([]byte, error) {
	meta := frontmatter{
		ID:       iss.ID,
		Status:   string(iss.Status),
		Deps:     iss.Dependencies,
		Links:    iss.Links,
		Created:  iss.Created,
		Type:     iss.Type,
		Priority: iss.Priority,
		Labels:   iss.Labels,
	}

	// Ensure empty slices are serialized as []
	if meta.Deps == nil {
		meta.Deps = []string{}
	}
	if meta.Links == nil {
		meta.Links = []string{}
	}

	fm, err := yaml.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshal frontmatter: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(fm)
	buf.WriteString("---\n")
	buf.WriteString("# ")
	buf.WriteString(iss.Title)
	buf.WriteString("\n\n")
	if iss.Description != "" {
		buf.WriteString(iss.Description)
		buf.WriteString("\n")
	}

	return buf.Bytes(), nil
}
