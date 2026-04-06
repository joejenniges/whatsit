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
