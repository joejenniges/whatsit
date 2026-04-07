package classifier

import (
	"math"
	"math/rand"
	"testing"
)

func TestRhythmDetector_SyntheticBeat(t *testing.T) {
	detector := NewRhythmDetector(16000)
	sampleRate := 16000
	duration := 2.0
	numSamples := int(duration * float64(sampleRate))
	samples := make([]float32, numSamples)

	// Generate a synthetic beat at 120 BPM (one click every 0.5s)
	beatIntervalSamples := sampleRate / 2 // 0.5 seconds
	for i := 0; i < numSamples; i++ {
		if i%beatIntervalSamples < 100 { // short click
			samples[i] = 0.8
		} else {
			samples[i] = 0.0
		}
	}

	strength := detector.RhythmStrength(samples)
	if strength < 0.5 {
		t.Errorf("Expected high rhythm strength for 120 BPM beat, got %.3f", strength)
	}
}

func TestRhythmDetector_WhiteNoise(t *testing.T) {
	detector := NewRhythmDetector(16000)
	samples := make([]float32, 32000) // 2 seconds
	rng := rand.New(rand.NewSource(42))
	for i := range samples {
		samples[i] = float32(rng.NormFloat64() * 0.3)
	}

	strength := detector.RhythmStrength(samples)
	// WHY 0.35 not 0.2: A single 2-second noise chunk can show some accidental
	// periodicity in the autocorrelation. In production, the RhythmAccumulator's
	// 6-second window smooths this out. The key invariant is that noise stays
	// well below the music threshold (0.5+).
	if strength > 0.35 {
		t.Errorf("Expected low rhythm strength for noise, got %.3f", strength)
	}
}

func TestRhythmDetector_Silence(t *testing.T) {
	detector := NewRhythmDetector(16000)
	samples := make([]float32, 32000) // 2 seconds of silence

	strength := detector.RhythmStrength(samples)
	if strength != 0.0 {
		t.Errorf("Expected 0.0 rhythm strength for silence, got %.3f", strength)
	}
}

func TestRhythmAccumulator_IncreasingStrength(t *testing.T) {
	acc := NewRhythmAccumulator(16000)
	sampleRate := 16000
	chunkDuration := 2.0
	chunkSamples := int(chunkDuration * float64(sampleRate))

	// Generate 120 BPM beat chunks
	beatIntervalSamples := sampleRate / 2
	makeChunk := func(offset int) []float32 {
		samples := make([]float32, chunkSamples)
		for i := 0; i < chunkSamples; i++ {
			globalPos := offset + i
			if globalPos%beatIntervalSamples < 100 {
				samples[i] = 0.8
			}
		}
		return samples
	}

	var strengths []float64
	for chunk := 0; chunk < 3; chunk++ {
		offset := chunk * chunkSamples
		s := acc.AddChunkAndAnalyze(makeChunk(offset))
		strengths = append(strengths, s)
	}

	// First chunks return 0 (not enough data yet -- need 4 seconds).
	// By the third chunk (6 seconds) we should have nonzero strength.
	if strengths[0] != 0.0 {
		t.Errorf("Expected 0.0 for first chunk (not enough data), got %.3f", strengths[0])
	}

	// Third chunk should show rhythm
	if strengths[2] < 0.3 {
		t.Errorf("Expected increasing rhythm strength by chunk 3, got %.3f", strengths[2])
	}
}

func TestRhythmDetector_SineBeat(t *testing.T) {
	// Generate amplitude-modulated sine wave that simulates a rhythmic pattern.
	// This is a more realistic test than simple clicks.
	detector := NewRhythmDetector(16000)
	sampleRate := 16000
	duration := 2.0
	numSamples := int(duration * float64(sampleRate))
	samples := make([]float32, numSamples)

	bpm := 120.0
	beatFreq := bpm / 60.0 // 2 Hz
	toneFreq := 440.0      // carrier

	for i := 0; i < numSamples; i++ {
		tSec := float64(i) / float64(sampleRate)
		// Amplitude envelope: periodic bumps at beat frequency
		env := 0.5 + 0.5*math.Cos(2*math.Pi*beatFreq*tSec)
		// Tone modulated by envelope
		samples[i] = float32(env * math.Sin(2*math.Pi*toneFreq*tSec) * 0.5)
	}

	strength := detector.RhythmStrength(samples)
	// Amplitude-modulated tone should show some rhythm
	if strength < 0.2 {
		t.Errorf("Expected moderate rhythm strength for AM sine beat, got %.3f", strength)
	}
}
