package classifier

import "log"

// debugLog writes a formatted classifier debug line via the log package.
// WHY log.Printf not fmt.Fprintf(os.Stderr): the log package writes to the
// configured log output (the log file). fmt.Fprintf(os.Stderr) writes to the
// raw file descriptor which may not flush to disk reliably.
func debugLog(tier string, format string, args ...interface{}) {
	log.Printf("[CLASSIFY/%s] "+format, append([]interface{}{tier}, args...)...)
}
