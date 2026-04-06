package transcriber

import "math"

// NeMo preprocessor parameters for Parakeet models.
// These match the NeMo AudioToMelSpectrogramPreprocessor defaults used by
// nvidia/parakeet-ctc-0.6b and the onnx-asr reference implementation.
const (
	melNFFT       = 512
	melWinLength  = 400 // 25ms at 16kHz
	melHopLength  = 160 // 10ms at 16kHz
	melPreemph    = 0.97
	melLogGuard   = 5.960464477539063e-8 // 2^-24
	melSampleRate = 16000
)

// melFilterbank holds precomputed triangular mel filter weights.
// Shape: [n_fft/2+1][n_mels]. Stored transposed from the typical convention
// so we can multiply power_spectrum @ filterbank directly.
type melFilterbank struct {
	weights [][]float64 // [n_fft/2+1][n_mels]
	nMels   int
	nFFT    int
}

func hzToMel(hz float64) float64 {
	return 2595.0 * math.Log10(1.0+hz/700.0)
}

func melToHz(mel float64) float64 {
	return 700.0 * (math.Pow(10.0, mel/2595.0) - 1.0)
}

// newMelFilterbank builds an [nFFT/2+1][nMels] filterbank matrix.
// This is the right-multiply form: spectrum @ filterbank -> mel_energies.
func newMelFilterbank(nMels, nFFT, sampleRate int) *melFilterbank {
	bins := nFFT/2 + 1
	melMin := hzToMel(0)
	melMax := hzToMel(float64(sampleRate) / 2.0)

	nPoints := nMels + 2
	melPoints := make([]float64, nPoints)
	for i := range melPoints {
		melPoints[i] = melMin + float64(i)*(melMax-melMin)/float64(nPoints-1)
	}

	binIndices := make([]int, nPoints)
	for i, m := range melPoints {
		hz := melToHz(m)
		binIndices[i] = int(math.Floor((float64(nFFT) + 1.0) * hz / float64(sampleRate)))
	}

	// Build [bins][nMels] matrix.
	weights := make([][]float64, bins)
	for j := range weights {
		weights[j] = make([]float64, nMels)
	}

	for i := 0; i < nMels; i++ {
		left := binIndices[i]
		center := binIndices[i+1]
		right := binIndices[i+2]

		for j := left; j <= center && j < bins; j++ {
			if j < 0 {
				continue
			}
			if center != left {
				weights[j][i] = float64(j-left) / float64(center-left)
			}
		}
		for j := center; j <= right && j < bins; j++ {
			if j < 0 {
				continue
			}
			if right != center {
				weights[j][i] = float64(right-j) / float64(right-center)
			}
		}
	}

	return &melFilterbank{weights: weights, nMels: nMels, nFFT: nFFT}
}

// computeLogMelSpectrogram implements the NeMo AudioToMelSpectrogramPreprocessor
// pipeline: preemphasis -> pad -> STFT -> power spectrum -> mel filterbank ->
// log -> per-feature normalization (zero mean, unit variance).
//
// Returns a flat float32 slice in [nMels][nFrames] layout (transposed from the
// computation order of [nFrames][nMels]) plus the number of frames.
//
// WHY per-feature normalization: NeMo's default preprocessor uses
// normalize="per_feature" which computes mean/var per mel band across time
// and normalizes. Without this, the model produces garbage output.
func computeLogMelSpectrogram(samples []float32, sampleRate, nMels int, fb *melFilterbank) ([]float32, int) {
	if len(samples) == 0 {
		return nil, 0
	}

	nFFT := melNFFT
	winLen := melWinLength
	hopLen := melHopLength
	bins := nFFT/2 + 1

	// 1. Preemphasis: y[n] = x[n] - 0.97 * x[n-1]
	preemph := make([]float64, len(samples))
	preemph[0] = float64(samples[0])
	for i := 1; i < len(samples); i++ {
		preemph[i] = float64(samples[i]) - melPreemph*float64(samples[i-1])
	}

	// 2. Pad with nFFT/2 on each side (NeMo uses zero padding).
	padLeft := nFFT / 2
	padRight := nFFT / 2
	padded := make([]float64, padLeft+len(preemph)+padRight)
	for i, v := range preemph {
		padded[padLeft+i] = v
	}

	// 3. Build Hanning window padded to nFFT.
	// WHY: NeMo uses a win_length=400 Hanning window zero-padded to nFFT=512.
	// The padding centers the window within the FFT frame.
	window := make([]float64, nFFT)
	padOffset := (nFFT - winLen) / 2
	for i := 0; i < winLen; i++ {
		window[padOffset+i] = 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(i)/float64(winLen)))
	}

	// 4. STFT: sliding window -> windowed frame -> FFT -> power spectrum.
	nFrames := (len(padded) - nFFT) / hopLen
	if nFrames <= 0 {
		return nil, 0
	}

	// Allocate [nFrames][nMels] for mel energies.
	logMel := make([][]float64, nFrames)

	for f := 0; f < nFrames; f++ {
		start := f * hopLen
		frame := padded[start : start+nFFT]

		// Apply window.
		windowed := make([]float64, nFFT)
		for i := range windowed {
			windowed[i] = frame[i] * window[i]
		}

		// Real FFT via DFT (we only need bins = nFFT/2+1 outputs).
		real := make([]float64, bins)
		imag := make([]float64, bins)
		for k := 0; k < bins; k++ {
			var re, im float64
			for n := 0; n < nFFT; n++ {
				angle := -2.0 * math.Pi * float64(k) * float64(n) / float64(nFFT)
				re += windowed[n] * math.Cos(angle)
				im += windowed[n] * math.Sin(angle)
			}
			real[k] = re
			imag[k] = im
		}

		// Power spectrum = |FFT|^2.
		power := make([]float64, bins)
		for k := 0; k < bins; k++ {
			power[k] = real[k]*real[k] + imag[k]*imag[k]
		}

		// Mel filterbank: matmul power @ filterbank -> [nMels].
		mel := make([]float64, nMels)
		for m := 0; m < nMels; m++ {
			var sum float64
			for k := 0; k < bins; k++ {
				sum += power[k] * fb.weights[k][m]
			}
			mel[m] = sum
		}

		// Log with guard.
		for m := range mel {
			mel[m] = math.Log(mel[m] + melLogGuard)
		}

		logMel[f] = mel
	}

	// 5. Per-feature normalization: for each mel band, compute mean and
	// variance across time, then normalize to zero mean / unit variance.
	nFramesF := float64(nFrames)
	means := make([]float64, nMels)
	for f := 0; f < nFrames; f++ {
		for m := 0; m < nMels; m++ {
			means[m] += logMel[f][m]
		}
	}
	for m := range means {
		means[m] /= nFramesF
	}

	vars := make([]float64, nMels)
	for f := 0; f < nFrames; f++ {
		for m := 0; m < nMels; m++ {
			d := logMel[f][m] - means[m]
			vars[m] += d * d
		}
	}
	// WHY nFrames-1: NeMo uses sample variance (Bessel's correction).
	if nFrames > 1 {
		for m := range vars {
			vars[m] /= nFramesF - 1.0
		}
	}

	// Normalize and transpose to [nMels][nFrames].
	out := make([]float32, nMels*nFrames)
	for f := 0; f < nFrames; f++ {
		for m := 0; m < nMels; m++ {
			normalized := (logMel[f][m] - means[m]) / (math.Sqrt(vars[m]) + 1e-5)
			out[m*nFrames+f] = float32(normalized)
		}
	}

	return out, nFrames
}
