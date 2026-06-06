package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"time"
)

const downloadProgressInterval = 2 * time.Second

func readDownloadBodyWithProgress(r io.Reader, total int64) ([]byte, error) {
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
}

func newDownloadProgress(w io.Writer, total int64) *downloadProgress {
	p := &downloadProgress{w: w, total: total}
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
}

func (p *downloadProgress) emit(prefix string) {
	if p.w == nil {
		return
	}
	p.last = time.Now()
	if p.total > 0 {
		percent := float64(p.read) / float64(p.total) * 100
		fmt.Fprintf(p.w, "%s: %s/%s (%.0f%%)\n", prefix, formatDownloadBytes(p.read), formatDownloadBytes(p.total), percent)
		return
	}
	fmt.Fprintf(p.w, "%s: %s\n", prefix, formatDownloadBytes(p.read))
}

func formatDownloadBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for next := n / unit; next >= unit && exp < 4; next /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
