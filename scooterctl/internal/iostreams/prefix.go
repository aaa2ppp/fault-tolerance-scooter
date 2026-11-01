package iostreams

import (
	"bytes"
	"io"
	"sync"
	"time"
)

type PrefixWriter struct {
	w             io.Writer
	prefix        []byte
	prefixTail    []byte
	prefixWritten bool
}

func NewPrefixWriter(w io.Writer, prefix string) *PrefixWriter {
	return &PrefixWriter{w: w, prefix: []byte(prefix)}
}

func (w *PrefixWriter) Write(b []byte) (int, error) {
	var n int

	for len(b) > 0 {
		if !w.prefixWritten {
			if len(w.prefixTail) == 0 {
				w.prefixTail = w.prefix
			}
			n2, err := w.w.Write(w.prefixTail)
			w.prefixTail = w.prefixTail[n2:]
			if err != nil {
				return n, err
			}
			if len(w.prefixTail) != 0 {
				return n, io.ErrShortWrite
			}
			w.prefixWritten = true
		}

		i := bytes.IndexByte(b, '\n') + 1
		if i == 0 {
			n2, err := w.w.Write(b)
			n += n2
			return n, err
		}

		n2, err := w.w.Write(b[:i])
		n += n2
		b = b[n2:]
		if err != nil {
			return n, err
		}
		if n2 != i {
			return n, io.ErrShortWrite
		}
		w.prefixWritten = false
	}

	return n, nil
}

type TimestampWriter struct {
	pw     PrefixWriter
	layout string
}

func NewTimestampWriter(w io.Writer, layout string) *TimestampWriter {
	return &TimestampWriter{
		pw:     PrefixWriter{w: w},
		layout: layout,
	}
}

func (w *TimestampWriter) Write(b []byte) (int, error) {
	if len(w.pw.prefixTail) == 0 {
		w.pw.prefix = time.Now().AppendFormat(w.pw.prefix[:0], w.layout)
	} else {
		w.pw.prefix = []byte(time.Now().Format(w.layout))
	}
	return w.pw.Write(b)
}

type LineWriter struct {
	w   io.Writer
	buf []byte
}

func NewLineWriter(w io.Writer) *LineWriter {
	return &LineWriter{w: w}
}

func (w *LineWriter) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	w.buf = append(w.buf, b...)
	i := bytes.LastIndexByte(w.buf, '\n') + 1
	if i > 0 {
		n, err := w.w.Write(w.buf[:i])
		n2 := copy(w.buf, w.buf[n:])
		w.buf = w.buf[:n2]
		if err != nil {
			return len(b), err
		}
		if n != i {
			return len(b), io.ErrShortWrite
		}
	}
	return len(b), nil
}

func (w *LineWriter) Flush() error {
	if len(w.buf) == 0 {
		return nil
	}
	if w.buf[len(w.buf)-1] != '\n' {
		w.buf = append(w.buf, '\n')
	}
	n, err := w.w.Write(w.buf)
	n2 := copy(w.buf, w.buf[n:])
	w.buf = w.buf[:n2]
	if err != nil {
		return err
	}
	if n != len(w.buf) {
		return io.ErrShortWrite
	}
	return nil
}

type SafeWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func NewSafeWriter(w io.Writer) *SafeWriter {
	return &SafeWriter{w: w}
}

func (w *SafeWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(b)
}
