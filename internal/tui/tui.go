package tui

import "io"

func Run(w io.Writer) error {
	_, err := w.Write([]byte("TUI is planned for v0.2. Use sample top/explain for v0.1.\n"))
	return err
}
