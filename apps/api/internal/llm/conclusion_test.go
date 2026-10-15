package llm

import "testing"

func TestConclusionValidation(t *testing.T) {
	valid := Conclusion{Text: "优先验证首次调用", Kind: "recommendation", SourceIDs: []string{"history:1"}, Rationale: "用户优先关注留存", Limitations: "需要实验验证"}
	if err := ValidateConclusions([]Conclusion{valid}); err != nil {
		t.Fatal(err)
	}

	for _, change := range []func(*Conclusion){
		func(c *Conclusion) { c.Kind = "certain" },
		func(c *Conclusion) { c.SourceIDs = nil },
		func(c *Conclusion) { c.Limitations = "" },
		func(c *Conclusion) { c.Rationale = " " },
		func(c *Conclusion) { c.SourceIDs = []string{"history:1", "history:1"} },
	} {
		candidate := valid
		change(&candidate)
		if err := ValidateConclusions([]Conclusion{candidate}); err == nil {
			t.Fatal("invalid conclusion accepted")
		}
	}

	if err := ValidateConclusions(nil); err != nil {
		t.Fatal("plain chat rejected")
	}
}
