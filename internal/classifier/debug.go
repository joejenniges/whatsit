package classifier

import (
	"fmt"
	"os"
)

// debugLog writes a formatted classifier debug line to stderr.
// Format: [CLASSIFY/tier] message
func debugLog(tier string, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "[CLASSIFY/%s] %s\n", tier, msg)
}
