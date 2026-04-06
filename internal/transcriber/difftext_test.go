package transcriber

import "testing"

func TestDiffText_Overlapping(t *testing.T) {
	prev := "The weather today will be"
	curr := "weather today will be sunny with highs"
	got := diffText(prev, curr)
	want := "sunny with highs"
	if got != want {
		t.Errorf("diffText(%q, %q) = %q, want %q", prev, curr, got, want)
	}
}

func TestDiffText_NoOverlap(t *testing.T) {
	prev := "completely different text"
	curr := "nothing in common here"
	got := diffText(prev, curr)
	// No overlap -- should return full current text.
	if got != curr {
		t.Errorf("diffText(%q, %q) = %q, want %q", prev, curr, got, curr)
	}
}

func TestDiffText_EmptyPrev(t *testing.T) {
	got := diffText("", "hello world")
	if got != "hello world" {
		t.Errorf("diffText empty prev = %q, want %q", got, "hello world")
	}
}

func TestDiffText_EmptyCurr(t *testing.T) {
	got := diffText("hello world", "")
	if got != "" {
		t.Errorf("diffText empty curr = %q, want %q", got, "")
	}
}

func TestDiffText_BothEmpty(t *testing.T) {
	got := diffText("", "")
	if got != "" {
		t.Errorf("diffText both empty = %q, want %q", got, "")
	}
}

func TestDiffText_Identical(t *testing.T) {
	text := "the quick brown fox"
	got := diffText(text, text)
	if got != "" {
		t.Errorf("diffText identical = %q, want %q", got, "")
	}
}

func TestDiffText_CurrentSubsetOfPrev(t *testing.T) {
	prev := "the quick brown fox jumps"
	curr := "brown fox jumps"
	got := diffText(prev, curr)
	// Current is a suffix of prev -- no new content.
	if got != "" {
		t.Errorf("diffText subset = %q, want %q", got, "")
	}
}

func TestDiffText_SingleWordOverlap(t *testing.T) {
	prev := "hello world"
	curr := "world is great"
	got := diffText(prev, curr)
	want := "is great"
	if got != want {
		t.Errorf("diffText single overlap = %q, want %q", got, want)
	}
}

func TestDiffText_LongerOverlap(t *testing.T) {
	prev := "one two three four five"
	curr := "three four five six seven eight"
	got := diffText(prev, curr)
	want := "six seven eight"
	if got != want {
		t.Errorf("diffText longer overlap = %q, want %q", got, want)
	}
}

func TestDiffText_PartialWordNoMatch(t *testing.T) {
	// Words differ even though characters partially overlap.
	prev := "testing"
	curr := "test results"
	got := diffText(prev, curr)
	// No word-level overlap, return full current.
	if got != curr {
		t.Errorf("diffText partial word = %q, want %q", got, curr)
	}
}

func TestDiffText_PunctuationDifference(t *testing.T) {
	// Whisper adds punctuation in later window. "you." vs "you" + "by."
	prev := "one day you'll find that life has passed you."
	curr := "one day you'll find that life has passed you by."
	got := diffText(prev, curr)
	// "by." is a trivial 1-word addition where "you" matches "you." normalized.
	// Should be suppressed or return just "by."
	if got == curr {
		t.Errorf("diffText punctuation: should not return full text, got %q", got)
	}
}

func TestDiffText_NearDuplicate(t *testing.T) {
	// Same text with minor trailing difference.
	prev := "All your days spent now numbers do one day you'll find that life has passed you."
	curr := "All your days spent now numbers do one day you'll find that life has passed you by."
	got := diffText(prev, curr)
	// Should be empty or just "by." -- NOT the full line.
	if len(got) > 5 {
		t.Errorf("diffText near-duplicate: should suppress, got %q", got)
	}
}

func TestDiffText_OverlapWithCarryover(t *testing.T) {
	// Window 2 has old text leaking in from the overlap.
	prev := "one day you'll find that life has passed you by."
	curr := "And in the quiet still aside, you'll find that life has passed you by."
	got := diffText(prev, curr)
	// The overlap is "you'll find that life has passed you by."
	// New content is "And in the quiet still aside,"
	want := "And in the quiet still aside,"
	if got != want {
		t.Errorf("diffText carryover = %q, want %q", got, want)
	}
}

func TestDiffText_NormalizeWord(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"hello", "hello"},
		{"Hello.", "hello"},
		{"you.", "you"},
		{"\"word\"", "word"},
		{"(test)", "test"},
		{"by.", "by"},
	}
	for _, tc := range tests {
		got := normalizeWord(tc.input)
		if got != tc.want {
			t.Errorf("normalizeWord(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
