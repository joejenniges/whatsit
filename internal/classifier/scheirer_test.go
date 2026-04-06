package classifier

import (
	"math"
	"testing"
)

func TestScheirerClassifier_Name(t *testing.T) {
	c := NewScheirerClassifier(16000)
	if c.Name() != "scheirer" {
		t.Errorf("expected name 'scheirer', got %q", c.Name())
	}
}

func TestComputeScheirerFeatures_Silence(t *testing.T) {
	// Pure silence: all features should be ~0.
	samples := make([]float32, 32000) // 2s at 16kHz
	f := ComputeScheirerFeatures(samples, 16000)

	if f.SpectralCentroidVariance > 1e-6 {
		t.Errorf("silence centroid variance = %v, want ~0", f.SpectralCentroidVariance)
	}
	if f.SpectralFluxMean > 1e-6 {
		t.Errorf("silence flux mean = %v, want ~0", f.SpectralFluxMean)
	}
	if f.ZCRMean > 1e-6 {
		t.Errorf("silence ZCR mean = %v, want ~0", f.ZCRMean)
	}
	// Low-energy percent on silence: all frames have equal (zero) energy,
	// none strictly below mean, so should be 0.
	if f.LowEnergyPercent > 0.01 {
		t.Errorf("silence low-energy %% = %v, want ~0", f.LowEnergyPercent)
	}
}

func TestComputeScheirerFeatures_SteadyTone(t *testing.T) {
	// A continuous pure tone simulates music: low centroid variance,
	// low low-energy %, stable spectrum.
	const sampleRate = 16000
	const freq = 440.0
	samples := make([]float32, 32000)
	for i := range samples {
		samples[i] = 0.3 * float32(math.Sin(2*math.Pi*freq*float64(i)/float64(sampleRate)))
	}

	f := ComputeScheirerFeatures(samples, sampleRate)

	// Centroid variance should be very low for a steady tone.
	if f.SpectralCentroidVariance > 1000 {
		t.Errorf("steady tone centroid variance = %v, want < 1000", f.SpectralCentroidVariance)
	}

	// Low-energy %: all frames have similar energy, so roughly half
	// will be below mean (close to 50%, not 60-80%).
	if f.LowEnergyPercent > 0.60 {
		t.Errorf("steady tone low-energy %% = %v, want < 0.60", f.LowEnergyPercent)
	}

	// ZCR should be consistent but not extreme for a 440 Hz tone at 16kHz.
	// Expected ~2*440/16000 = 0.055
	if f.ZCRMean < 0.03 || f.ZCRMean > 0.10 {
		t.Errorf("steady tone ZCR mean = %v, want 0.03-0.10", f.ZCRMean)
	}
}

func TestComputeScheirerFeatures_SimulatedSpeech(t *testing.T) {
	// Alternating tone + silence simulates speech: high centroid variance,
	// high low-energy %, varying spectral content.
	const sampleRate = 16000
	const freq = 500.0
	samples := make([]float32, 32000)

	for i := range samples {
		// 200ms tone, 200ms silence, repeating.
		// At 16kHz, 200ms = 3200 samples.
		block := (i / 3200) % 2
		if block == 0 {
			samples[i] = 0.4 * float32(math.Sin(2*math.Pi*freq*float64(i)/float64(sampleRate)))
		}
		// else: leave as 0 (silence)
	}

	f := ComputeScheirerFeatures(samples, sampleRate)

	// Centroid variance should be high: shifts between ~500Hz and ~0Hz.
	if f.SpectralCentroidVariance < 10000 {
		t.Errorf("speech-like centroid variance = %v, want > 10000", f.SpectralCentroidVariance)
	}

	// Low-energy %: roughly half the frames are silent, so should be well above 55%.
	if f.LowEnergyPercent < 0.40 {
		t.Errorf("speech-like low-energy %% = %v, want > 0.40", f.LowEnergyPercent)
	}

	// Flux should be non-trivial due to transitions.
	if f.SpectralFluxMean < 0.01 {
		t.Errorf("speech-like flux mean = %v, want > 0.01", f.SpectralFluxMean)
	}
}

func TestLowEnergyPercent_KnownValues(t *testing.T) {
	tests := []struct {
		name     string
		energies []float64
		want     float64
	}{
		{
			name:     "empty",
			energies: nil,
			want:     0,
		},
		{
			name:     "all equal",
			energies: []float64{1.0, 1.0, 1.0, 1.0},
			want:     0, // none strictly below mean
		},
		{
			name:     "half low",
			energies: []float64{0.0, 0.0, 1.0, 1.0},
			want:     0.5, // mean=0.5, two values below
		},
		{
			name:     "mostly low",
			energies: []float64{0.0, 0.0, 0.0, 0.0, 10.0},
			want:     0.8, // mean=2.0, four values below
		},
		{
			name:     "single value",
			energies: []float64{5.0},
			want:     0, // not below its own mean
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LowEnergyPercent(tt.energies)
			if math.Abs(got-tt.want) > 0.01 {
				t.Errorf("LowEnergyPercent(%v) = %v, want %v", tt.energies, got, tt.want)
			}
		})
	}
}

func TestScheirerClassifier_Debounce(t *testing.T) {
	c := NewScheirerClassifier(16000)
	// debounceN defaults to 5.

	// Create a signal that the classifier will read as speech:
	// alternating loud tone + silence with high energy variance.
	speechSamples := makeScheirerSpeechLike(32000)

	// The classifier starts at ClassSilence.
	// Feed speech signals: should not switch until debounce count met.

	// Calls 1-4: should still be silence (debouncing).
	for i := 1; i <= 4; i++ {
		r := c.Classify(speechSamples)
		if r != ClassSilence {
			t.Errorf("debounce call %d: expected ClassSilence (still debouncing), got %s", i, r)
		}
	}

	// Call 5: should switch.
	r := c.Classify(speechSamples)
	if r == ClassSilence {
		t.Errorf("debounce call 5: expected non-silence after 5 consecutive, got %s", r)
	}
}

func TestScheirerClassifier_SilenceImmediate(t *testing.T) {
	c := NewScheirerClassifier(16000)

	// Silence should transition immediately regardless of debounce.
	silence := make([]float32, 32000)
	r := c.Classify(silence)
	if r != ClassSilence {
		t.Errorf("expected ClassSilence for silent input, got %s", r)
	}
}

func TestScheirerClassifier_SteadyToneIsMusic(t *testing.T) {
	c := NewScheirerClassifier(16000)
	c.debounceN = 1 // disable debounce for this test

	const freq = 440.0
	samples := make([]float32, 32000)
	for i := range samples {
		samples[i] = 0.3 * float32(math.Sin(2*math.Pi*freq*float64(i)/16000.0))
	}

	r := c.Classify(samples)
	if r != ClassMusic {
		t.Errorf("expected ClassMusic for steady tone, got %s", r)
	}
}

func TestComputeScheirerFeatures_TooShort(t *testing.T) {
	// Input shorter than one frame should return zero features.
	samples := make([]float32, 100) // well under 400 (25ms at 16kHz)
	f := ComputeScheirerFeatures(samples, 16000)

	if f.SpectralCentroidVariance != 0 || f.SpectralFluxMean != 0 ||
		f.ZCRMean != 0 || f.LowEnergyPercent != 0 {
		t.Errorf("expected zero features for short input, got %+v", f)
	}
}

// makeScheirerSpeechLike generates a signal with properties that push
// the Scheirer classifier toward speech: high centroid variance (tone
// alternating with silence), high low-energy %, varying ZCR.
func makeScheirerSpeechLike(n int) []float32 {
	samples := make([]float32, n)
	const freq = 800.0
	const sampleRate = 16000.0

	for i := range samples {
		// 200ms of tone, 200ms of near-silence, repeating.
		// This creates high centroid variance and high low-energy %.
		block := (i / 3200) % 2
		if block == 0 {
			samples[i] = 0.5 * float32(math.Sin(2*math.Pi*freq*float64(i)/sampleRate))
		} else {
			// Near-silence with tiny noise to avoid exact-zero ZCR issues.
			samples[i] = 0.001 * float32(math.Sin(2*math.Pi*200*float64(i)/sampleRate))
		}
	}
	return samples
}
