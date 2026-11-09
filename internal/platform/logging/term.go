package logging

import "os"

// ConsoleStyle tells how to write the log to f: in the readable form when f is a
// terminal, with colors unless the terminal cannot show them or NO_COLOR is set
// (https://no-color.org). Pipes, files and services get key=value lines, unless
// FORCE_COLOR asks for the readable form anyway (https://force-color.org).
func ConsoleStyle(f *os.File) (pretty, color bool) {
	noColor := os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb"
	if force := os.Getenv("FORCE_COLOR"); force != "" && force != "0" {
		if f != nil && isTerminal(f) {
			enableColor(f)
		}
		return true, !noColor
	}
	if f == nil || !isTerminal(f) {
		return false, false
	}
	return true, !noColor && enableColor(f)
}
