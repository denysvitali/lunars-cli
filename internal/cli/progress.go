package cli

import (
	"fmt"
	"io"
)

const progressStepBytes int64 = 32 * 1024 * 1024

type progressReader struct {
	reader   io.Reader
	out      io.Writer
	label    string
	total    int64
	written  int64
	next     int64
	reported bool
}

func NewProgressReader(reader io.Reader, total int64, out io.Writer, label string) io.Reader {
	return NewProgressReaderAt(reader, total, 0, out, label)
}

func NewProgressReaderAt(reader io.Reader, total, initial int64, out io.Writer, label string) io.Reader {
	if out == nil {
		return reader
	}
	return &progressReader{
		reader:  reader,
		out:     out,
		label:   label,
		total:   total,
		written: initial,
		next:    initial + progressStepBytes,
	}
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.written += int64(n)
		if r.total >= 0 && (r.written >= r.total || r.written >= r.next) {
			r.report(false)
			r.next = r.written + progressStepBytes
		}
	}
	if err == io.EOF {
		r.report(true)
	}
	return n, err
}

func (r *progressReader) report(final bool) {
	if r.reported && final {
		return
	}
	if r.total >= 0 {
		percent := 0.0
		if r.total > 0 {
			percent = float64(r.written) / float64(r.total) * 100
		}
		_, _ = fmt.Fprintf(r.out, "%s: %s/%s (%.0f%%)\n", r.label, formatBytes(r.written), formatBytes(r.total), percent)
	} else if final {
		_, _ = fmt.Fprintf(r.out, "%s: %s\n", r.label, formatBytes(r.written))
	}
	if final {
		r.reported = true
	}
}

func formatBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := int64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(div), "KMGTPE"[exp])
}
