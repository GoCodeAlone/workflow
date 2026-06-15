package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
)

const downloadProgressInterval = 2 * time.Second

var downloadProgressQuiet bool

func setDownloadProgressQuiet(quiet bool) func() {
	previous := downloadProgressQuiet
	downloadProgressQuiet = previous || quiet
	return func() {
		downloadProgressQuiet = previous
	}
}

func shouldSuppressDownloadProgress() bool {
	if downloadProgressQuiet {
		return true
	}
	value := strings.TrimSpace(strings.ToLower(os.Getenv("WFCTL_PLUGIN_INSTALL_QUIET")))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func readDownloadBodyWithProgress(r io.Reader, total int64) ([]byte, error) {
	if shouldSuppressDownloadProgress() {
		return io.ReadAll(r)
	}
	var buf bytes.Buffer
	tracker := newDownloadProgress(os.Stderr, total)
	if _, err := io.Copy(io.MultiWriter(&buf, tracker), r); err != nil {
		return nil, err
	}
	tracker.finish()
	return buf.Bytes(), nil
}

type downloadProgress struct {
	w     io.Writer
	total int64
	read  int64
	last  time.Time
	tty   bool
}

func newDownloadProgress(w io.Writer, total int64) *downloadProgress {
	return newDownloadProgressWithTerminal(w, total, isatty.IsTerminal(os.Stderr.Fd()))
}

func newDownloadProgressWithTerminal(w io.Writer, total int64, tty bool) *downloadProgress {
	p := &downloadProgress{w: w, total: total, tty: tty}
	p.emit("Download progress")
	return p
}

func (p *downloadProgress) Write(data []byte) (int, error) {
	n := len(data)
	p.read += int64(n)
	now := time.Now()
	if p.last.IsZero() || now.Sub(p.last) >= downloadProgressInterval || (p.total > 0 && p.read >= p.total) {
		p.emit("Download progress")
	}
	return n, nil
}

func (p *downloadProgress) finish() {
	p.emit("Download complete")
	if p.tty && p.w != nil {
		fmt.Fprintln(p.w)
	}
}

func (p *downloadProgress) emit(prefix string) {
	if p.w == nil {
		return
	}
	p.last = time.Now()
	line := ""
	if p.total > 0 {
		percent := float64(p.read) / float64(p.total) * 100
		line = fmt.Sprintf("%s: %s/%s (%.0f%%)", prefix, formatDownloadBytes(p.read), formatDownloadBytes(p.total), percent)
	} else {
		line = fmt.Sprintf("%s: %s", prefix, formatDownloadBytes(p.read))
	}
	if p.tty {
		fmt.Fprintf(p.w, "\r%s", line)
	} else {
		fmt.Fprintln(p.w, line)
	}
}

func formatDownloadBytes(n int64) string {
	const unit = 1024
	const prefixes = "KMGTPE"
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for next := n / unit; next >= unit && exp < len(prefixes)-1; next /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), prefixes[exp])
}
