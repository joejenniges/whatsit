package ui

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// modelSizeMB maps model size names to approximate file sizes in MB.
var modelSizeMB = map[string]int{
	"tiny":   75,
	"base":   142,
	"small":  466,
	"medium": 1533,
}

// DownloadScreen shows model download progress with speed and ETA.
type DownloadScreen struct {
	modelSize string

	titleLabel    *widget.Label
	subtitleLabel *widget.Label
	progressBar   *widget.ProgressBar
	statsLabel    *widget.Label
	cancelBtn     *widget.Button

	root fyne.CanvasObject

	// For speed/ETA calculation.
	startTime     time.Time
	lastBytes     int64
	lastCheckTime time.Time

	// OnCancel is called when the user clicks Cancel.
	OnCancel func()
}

// NewDownloadScreen creates a download progress screen for the given model size.
func NewDownloadScreen(modelSize string) *DownloadScreen {
	ds := &DownloadScreen{
		modelSize: modelSize,
		startTime: time.Now(),
	}

	sizeMB := modelSizeMB[modelSize]
	if sizeMB == 0 {
		sizeMB = 142 // default to base size
	}

	ds.titleLabel = widget.NewLabel("Downloading necessary components...")
	ds.titleLabel.Alignment = fyne.TextAlignCenter
	ds.titleLabel.TextStyle = fyne.TextStyle{Bold: true}

	ds.subtitleLabel = widget.NewLabel(
		fmt.Sprintf("Downloading ggml-%s.en.bin (%d MB)", modelSize, sizeMB),
	)
	ds.subtitleLabel.Alignment = fyne.TextAlignCenter

	ds.progressBar = widget.NewProgressBar()
	ds.progressBar.Min = 0
	ds.progressBar.Max = 1

	ds.statsLabel = widget.NewLabel("Starting download...")
	ds.statsLabel.Alignment = fyne.TextAlignCenter

	ds.cancelBtn = widget.NewButton("Cancel", func() {
		if ds.OnCancel != nil {
			ds.OnCancel()
		}
	})

	// Layout: centered vertical stack with some horizontal padding for the
	// progress bar.
	inner := container.NewVBox(
		ds.titleLabel,
		ds.subtitleLabel,
		layout.NewSpacer(),
		container.NewPadded(ds.progressBar),
		ds.statsLabel,
		layout.NewSpacer(),
		container.NewCenter(ds.cancelBtn),
	)

	// Constrain width so it doesn't stretch across the full window.
	constrained := container.New(
		layout.NewMaxLayout(),
		inner,
	)

	ds.root = container.NewCenter(
		container.New(
			layout.NewMaxLayout(),
			container.NewPadded(container.NewPadded(constrained)),
		),
	)

	return ds
}

// Container returns the root canvas object for this screen.
func (ds *DownloadScreen) Container() fyne.CanvasObject {
	return ds.root
}

// UpdateProgress updates the progress bar and stats text. Safe to call from
// any goroutine.
func (ds *DownloadScreen) UpdateProgress(downloaded, total int64) {
	fyne.Do(func() {
		if total <= 0 {
			ds.progressBar.SetValue(0)
			ds.statsLabel.SetText(fmt.Sprintf("Downloaded %s...", formatBytes(downloaded)))
			return
		}

		fraction := float64(downloaded) / float64(total)
		ds.progressBar.SetValue(fraction)

		elapsed := time.Since(ds.startTime).Seconds()
		var speedText, etaText string

		if elapsed > 0.5 {
			bytesPerSec := float64(downloaded) / elapsed
			speedText = fmt.Sprintf("%s/s", formatBytes(int64(bytesPerSec)))

			remaining := float64(total-downloaded) / bytesPerSec
			if remaining > 0 {
				etaText = formatDuration(time.Duration(remaining) * time.Second)
			} else {
				etaText = "finishing..."
			}
		} else {
			speedText = "calculating..."
			etaText = "calculating..."
		}

		pct := int(fraction * 100)
		ds.statsLabel.SetText(
			fmt.Sprintf("%d%% -- %s of %s -- %s -- ETA %s",
				pct,
				formatBytes(downloaded),
				formatBytes(total),
				speedText,
				etaText,
			),
		)
	})
}

// formatBytes returns a human-readable byte size.
func formatBytes(b int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// formatDuration returns a compact duration string like "2m 15s" or "45s".
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) - m*60
	return fmt.Sprintf("%dm %ds", m, s)
}
