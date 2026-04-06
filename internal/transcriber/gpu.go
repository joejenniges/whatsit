package transcriber

import (
	"os"
	"path/filepath"
)

// IsGPUAvailable checks if the Vulkan backend DLL is present next to the
// executable, which is a prerequisite for GPU acceleration. The MSYS2
// whisper.cpp package automatically uses Vulkan when ggml-vulkan.dll is
// found at runtime; its absence causes a CPU-only fallback.
func IsGPUAvailable() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	vulkanDLL := filepath.Join(filepath.Dir(exe), "ggml-vulkan.dll")
	_, err = os.Stat(vulkanDLL)
	return err == nil
}
