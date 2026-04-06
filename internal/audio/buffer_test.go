package audio

import (
	"sync"
	"testing"
	"time"
)

func TestBuffer_WriteRead(t *testing.T) {
	buf := NewBuffer(1000)

	// Write some samples.
	samples := []float32{0.1, 0.2, 0.3, 0.4, 0.5}
	buf.Write(samples)

	if buf.Len() != 5 {
		t.Fatalf("expected Len()=5, got %d", buf.Len())
	}

	// Read fewer than available.
	got := buf.Read(3)
	if len(got) != 3 {
		t.Fatalf("expected 3 samples, got %d", len(got))
	}
	if got[0] != 0.1 || got[1] != 0.2 || got[2] != 0.3 {
		t.Fatalf("unexpected samples: %v", got)
	}

	// Remaining should be 2.
	if buf.Len() != 2 {
		t.Fatalf("expected Len()=2, got %d", buf.Len())
	}

	// Read more than available.
	got = buf.Read(10)
	if len(got) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(got))
	}
	if got[0] != 0.4 || got[1] != 0.5 {
		t.Fatalf("unexpected samples: %v", got)
	}

	// Buffer should be empty.
	if buf.Len() != 0 {
		t.Fatalf("expected Len()=0, got %d", buf.Len())
	}
}

func TestBuffer_ReadAll(t *testing.T) {
	buf := NewBuffer(1000)
	buf.Write([]float32{1.0, 2.0, 3.0})

	got := buf.ReadAll()
	if len(got) != 3 {
		t.Fatalf("expected 3 samples, got %d", len(got))
	}
	if buf.Len() != 0 {
		t.Fatalf("expected empty buffer after ReadAll, got %d", buf.Len())
	}

	// ReadAll on empty buffer.
	got = buf.ReadAll()
	if got != nil {
		t.Fatalf("expected nil from empty ReadAll, got %v", got)
	}
}

func TestBuffer_Clear(t *testing.T) {
	buf := NewBuffer(1000)
	buf.Write([]float32{1.0, 2.0, 3.0, 4.0, 5.0})
	buf.Clear()

	if buf.Len() != 0 {
		t.Fatalf("expected Len()=0 after Clear, got %d", buf.Len())
	}
}

func TestBuffer_MaxSize(t *testing.T) {
	buf := NewBuffer(5)

	// Write more than maxSize.
	buf.Write([]float32{1, 2, 3, 4, 5, 6, 7, 8})

	if buf.Len() != 5 {
		t.Fatalf("expected Len()=5 (capped at maxSize), got %d", buf.Len())
	}

	// Should have kept the last 5 samples.
	got := buf.ReadAll()
	expected := []float32{4, 5, 6, 7, 8}
	for i, v := range expected {
		if got[i] != v {
			t.Fatalf("sample[%d]: expected %f, got %f", i, v, got[i])
		}
	}
}

func TestBuffer_Duration(t *testing.T) {
	buf := NewBuffer(100000)

	// Write exactly 1 second of audio at 16000 Hz.
	samples := make([]float32, 16000)
	buf.Write(samples)

	dur := buf.Duration(16000)
	if dur != 1*time.Second {
		t.Fatalf("expected 1s duration, got %v", dur)
	}

	// Write another half second.
	buf.Write(make([]float32, 8000))
	dur = buf.Duration(16000)
	expected := 1500 * time.Millisecond
	if dur != expected {
		t.Fatalf("expected %v duration, got %v", expected, dur)
	}

	// Zero sample rate should return 0.
	if buf.Duration(0) != 0 {
		t.Fatal("expected 0 duration for zero sample rate")
	}
}

func TestBuffer_ReadEmpty(t *testing.T) {
	buf := NewBuffer(100)
	got := buf.Read(10)
	if got != nil {
		t.Fatalf("expected nil from empty Read, got %v", got)
	}
}

func TestBuffer_ConcurrentWrites(t *testing.T) {
	buf := NewBuffer(100000)
	var wg sync.WaitGroup

	// Launch several goroutines writing concurrently.
	numWriters := 10
	samplesPerWriter := 1000
	wg.Add(numWriters)
	for i := 0; i < numWriters; i++ {
		go func() {
			defer wg.Done()
			samples := make([]float32, samplesPerWriter)
			for j := range samples {
				samples[j] = float32(j)
			}
			buf.Write(samples)
		}()
	}
	wg.Wait()

	// All samples should be present (buffer is large enough).
	if buf.Len() != numWriters*samplesPerWriter {
		t.Fatalf("expected %d samples, got %d", numWriters*samplesPerWriter, buf.Len())
	}
}

func TestBuffer_TrimFront(t *testing.T) {
	buf := NewBuffer(1000)
	buf.Write([]float32{1, 2, 3, 4, 5})

	buf.TrimFront(2)
	if buf.Len() != 3 {
		t.Fatalf("expected Len()=3 after TrimFront(2), got %d", buf.Len())
	}
	got := buf.ReadAll()
	expected := []float32{3, 4, 5}
	for i, v := range expected {
		if got[i] != v {
			t.Fatalf("sample[%d]: expected %f, got %f", i, v, got[i])
		}
	}
}

func TestBuffer_TrimFront_ExceedsLen(t *testing.T) {
	buf := NewBuffer(1000)
	buf.Write([]float32{1, 2, 3})

	buf.TrimFront(10)
	if buf.Len() != 0 {
		t.Fatalf("expected Len()=0 after TrimFront exceeding length, got %d", buf.Len())
	}
}

func TestBuffer_TrimFront_Zero(t *testing.T) {
	buf := NewBuffer(1000)
	buf.Write([]float32{1, 2, 3})

	buf.TrimFront(0)
	if buf.Len() != 3 {
		t.Fatalf("expected Len()=3 after TrimFront(0), got %d", buf.Len())
	}
}

func TestBuffer_TrimFront_ExactLen(t *testing.T) {
	buf := NewBuffer(1000)
	buf.Write([]float32{1, 2, 3})

	buf.TrimFront(3)
	if buf.Len() != 0 {
		t.Fatalf("expected Len()=0 after TrimFront(Len()), got %d", buf.Len())
	}
}

func TestBuffer_ConcurrentReadWrite(t *testing.T) {
	buf := NewBuffer(100000)
	var wg sync.WaitGroup

	// Writer goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			buf.Write(make([]float32, 100))
		}
	}()

	// Reader goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			buf.Read(50)
		}
	}()

	// Should not panic or deadlock.
	wg.Wait()
}
