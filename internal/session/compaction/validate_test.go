package compaction

import "testing"

func TestValidateSummaryRequiresHeadingsInOrder(t *testing.T) {
	ok := "# Continuation Handoff\n"
	for _, h := range requiredHeadings {
		ok += h + "\nUnknown.\n"
	}
	if validateSummary(ok, 5000) == "" {
		t.Fatal("expected valid")
	}
	if validateSummary("## Active Goal\n", 5000) != "" {
		t.Fatal("incomplete must fail")
	}
	if validateSummary(ok+"\n"+recallHeading, 5000) != "" {
		t.Fatal("recall heading in LLM output must fail")
	}
}

func TestOriginAllowed(t *testing.T) {
	allowed := []string{"https://api.openai.com"}
	if err := originAllowed("https://api.openai.com/v1/responses/compact", allowed); err != nil {
		t.Fatal(err)
	}
	if err := originAllowed("https://evil.example/v1/responses", allowed); err == nil {
		t.Fatal("expected deny")
	}
	if err := originAllowed("http://127.0.0.1:8080/v1/responses", allowed); err != nil {
		t.Fatal(err)
	}
}
