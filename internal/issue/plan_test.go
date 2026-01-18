package issue

import (
	"testing"
)

func TestUpsertPlanSection(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		planContent string
		want        string
	}{
		{
			name:        "empty body inserts plan",
			body:        "",
			planContent: "- [ ] Step 1\n- [ ] Step 2",
			want:        "## Plan\n\n- [ ] Step 1\n- [ ] Step 2\n",
		},
		{
			name:        "body without plan section appends plan",
			body:        "## Summary\n\nThis is the summary.",
			planContent: "- [ ] Step 1\n- [ ] Step 2",
			want:        "## Summary\n\nThis is the summary.\n\n## Plan\n\n- [ ] Step 1\n- [ ] Step 2\n",
		},
		{
			name:        "existing plan section is replaced",
			body:        "## Summary\n\nThis is the summary.\n\n## Plan\n\n- [ ] Old step 1\n- [ ] Old step 2\n",
			planContent: "- [ ] New step 1\n- [ ] New step 2",
			want:        "## Summary\n\nThis is the summary.\n\n## Plan\n\n- [ ] New step 1\n- [ ] New step 2\n",
		},
		{
			name:        "plan section between other sections",
			body:        "## Summary\n\nSummary text.\n\n## Plan\n\n- [ ] Old step\n\n## Notes\n\nSome notes.",
			planContent: "- [ ] New step",
			want:        "## Summary\n\nSummary text.\n\n## Plan\n\n- [ ] New step\n\n## Notes\n\nSome notes.\n",
		},
		{
			name:        "plan section at beginning",
			body:        "## Plan\n\n- [ ] Old step\n\n## Summary\n\nSummary text.",
			planContent: "- [ ] New step",
			want:        "## Plan\n\n- [ ] New step\n\n## Summary\n\nSummary text.\n",
		},
		{
			name:        "plan section at end with no trailing content",
			body:        "## Summary\n\nSummary text.\n\n## Plan\n\n- [ ] Old step",
			planContent: "- [ ] New step",
			want:        "## Summary\n\nSummary text.\n\n## Plan\n\n- [ ] New step\n",
		},
		{
			name:        "preserves content before and after",
			body:        "Introduction text.\n\n## Summary\n\nSummary.\n\n## Plan\n\nOld plan.\n\n## Testing\n\nTest instructions.\n\nFinal notes.",
			planContent: "New plan content.",
			want:        "Introduction text.\n\n## Summary\n\nSummary.\n\n## Plan\n\nNew plan content.\n\n## Testing\n\nTest instructions.\n\nFinal notes.\n",
		},
		{
			name:        "handles CRLF line endings",
			body:        "## Summary\r\n\r\nText.\r\n\r\n## Plan\r\n\r\nOld.",
			planContent: "New.",
			want:        "## Summary\n\nText.\n\n## Plan\n\nNew.\n",
		},
		{
			name:        "strips extra whitespace from plan content",
			body:        "## Summary\n\nText.",
			planContent: "  \n\n- [ ] Step 1\n\n  ",
			want:        "## Summary\n\nText.\n\n## Plan\n\n- [ ] Step 1\n",
		},
		{
			name:        "idempotent - same content produces same result",
			body:        "## Plan\n\n- [ ] Step 1\n",
			planContent: "- [ ] Step 1",
			want:        "## Plan\n\n- [ ] Step 1\n",
		},
		{
			name:        "handles body with only whitespace",
			body:        "   \n\n  ",
			planContent: "- [ ] Step 1",
			want:        "## Plan\n\n- [ ] Step 1\n",
		},
		{
			name:        "plan heading without content is replaced",
			body:        "## Summary\n\nText.\n\n## Plan\n\n## Notes\n\nNotes.",
			planContent: "- [ ] New step",
			want:        "## Summary\n\nText.\n\n## Plan\n\n- [ ] New step\n\n## Notes\n\nNotes.\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UpsertPlanSection(tt.body, tt.planContent)
			if got != tt.want {
				t.Errorf("UpsertPlanSection() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestUpsertPlanSection_Idempotent(t *testing.T) {
	// Verify that applying the same plan content twice gives the same result
	body := "## Summary\n\nThis is a test.\n"
	planContent := "- [ ] Step 1\n- [ ] Step 2"

	result1 := UpsertPlanSection(body, planContent)
	result2 := UpsertPlanSection(result1, planContent)

	if result1 != result2 {
		t.Errorf("UpsertPlanSection is not idempotent:\nfirst call:\n%q\nsecond call:\n%q", result1, result2)
	}
}
