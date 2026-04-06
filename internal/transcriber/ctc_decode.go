package transcriber

import "strings"

// ctcGreedyDecode performs greedy CTC decoding: argmax per frame, collapse
// repeated tokens, remove blank token (blankID).
//
// logits is a flat slice in [nFrames][vocabSize] layout.
// vocab maps token IDs to strings. Parakeet uses sentencepiece-style tokens
// where the "▁" (U+2581) prefix denotes a word boundary (space).
func ctcGreedyDecode(logits []float32, nFrames, vocabSize int, vocab []string, blankID int) string {
	if nFrames == 0 || vocabSize == 0 {
		return ""
	}

	prevToken := -1
	var tokens []string

	for f := 0; f < nFrames; f++ {
		// Argmax over vocab dimension.
		offset := f * vocabSize
		bestID := 0
		bestVal := logits[offset]
		for v := 1; v < vocabSize; v++ {
			if logits[offset+v] > bestVal {
				bestVal = logits[offset+v]
				bestID = v
			}
		}

		// Skip blank and repeated tokens.
		if bestID == blankID || bestID == prevToken {
			prevToken = bestID
			continue
		}
		prevToken = bestID

		if bestID < len(vocab) {
			tokens = append(tokens, vocab[bestID])
		}
	}

	// Join tokens and convert sentencepiece "▁" markers to spaces.
	raw := strings.Join(tokens, "")
	raw = strings.ReplaceAll(raw, "\u2581", " ")
	return strings.TrimSpace(raw)
}
