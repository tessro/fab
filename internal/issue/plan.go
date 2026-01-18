package issue

import (
	"regexp"
	"strings"
)

// PlanTemplate is the default template for plan sections.
// Each line should be a bullet point describing an implementation step.
const PlanTemplate = `## Plan

- [ ] Step 1: Investigate and understand the current implementation
- [ ] Step 2: Design the solution
- [ ] Step 3: Implement the changes
- [ ] Step 4: Write tests
- [ ] Step 5: Update documentation if needed
`

// planHeadingRegex matches ## Plan or ## Plan followed by whitespace/content.
var planHeadingRegex = regexp.MustCompile(`(?m)^## Plan\s*$`)

// sectionHeadingRegex matches any ## heading (used to find end of Plan section).
var sectionHeadingRegex = regexp.MustCompile(`(?m)^## [^\n]+$`)

// UpsertPlanSection updates or inserts a ## Plan section in a Markdown body.
// If a ## Plan section exists, it replaces the content between the heading and
// the next ## heading (or end of document).
// If no ## Plan section exists, it appends one at the end of the body.
//
// planContent should be the content to place under the ## Plan heading (without
// the heading itself). The function handles proper spacing.
//
// This function is idempotent - calling it with the same content will produce
// the same result.
func UpsertPlanSection(body, planContent string) string {
	// Normalize line endings
	body = strings.ReplaceAll(body, "\r\n", "\n")
	planContent = strings.ReplaceAll(planContent, "\r\n", "\n")

	// Find if ## Plan section exists
	planLoc := planHeadingRegex.FindStringIndex(body)

	if planLoc == nil {
		// No existing Plan section - append at end
		return appendPlanSection(body, planContent)
	}

	// Find where the Plan section ends (next ## heading or end of doc)
	afterPlan := body[planLoc[1]:]
	nextSectionLoc := sectionHeadingRegex.FindStringIndex(afterPlan)

	var beforePlan, afterSection string
	beforePlan = body[:planLoc[0]]

	if nextSectionLoc == nil {
		// Plan section goes to end of document
		afterSection = ""
	} else {
		afterSection = afterPlan[nextSectionLoc[0]:]
	}

	// Build new body with replaced Plan section
	return buildPlanSection(beforePlan, planContent, afterSection)
}

// appendPlanSection appends a Plan section to the end of the body.
func appendPlanSection(body, planContent string) string {
	body = strings.TrimRight(body, "\n\t ")

	var sb strings.Builder
	sb.WriteString(body)

	// Add spacing before new section
	if len(body) > 0 {
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Plan\n\n")
	sb.WriteString(strings.TrimSpace(planContent))
	sb.WriteString("\n")

	return sb.String()
}

// buildPlanSection builds the body with the Plan section content.
func buildPlanSection(beforePlan, planContent, afterSection string) string {
	beforePlan = strings.TrimRight(beforePlan, "\n\t ")
	planContent = strings.TrimSpace(planContent)
	afterSection = strings.TrimRight(strings.TrimLeft(afterSection, "\n\t "), "\n\t ")

	var sb strings.Builder

	// Content before Plan section
	if len(beforePlan) > 0 {
		sb.WriteString(beforePlan)
		sb.WriteString("\n\n")
	}

	// Plan section
	sb.WriteString("## Plan\n\n")
	sb.WriteString(planContent)

	// Content after Plan section
	if len(afterSection) > 0 {
		sb.WriteString("\n\n")
		sb.WriteString(afterSection)
	}

	// Always end with a single newline
	sb.WriteString("\n")

	return sb.String()
}
