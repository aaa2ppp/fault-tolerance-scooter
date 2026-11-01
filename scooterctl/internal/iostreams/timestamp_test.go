package iostreams

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
)

var tsFmt = "2006-01-02 15:04:05 "
var tsRe = regexp.MustCompile(`(?m)^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} `)

func stripTS(s string) string {
	return tsRe.ReplaceAllString(s, "")
}

func TestTimestampWriter_Write(t *testing.T) {
	tests := []struct {
		name        string
		w           *strings.Builder
		chunks      []string
		want        int
		wantErr     bool
		wantContent string // expected output after stripping timestamps
	}{
		{
			"one line",
			new(strings.Builder),
			[]string{"one\n"},
			4,
			false,
			"one\n",
		},
		{
			"empty write",
			new(strings.Builder),
			[]string{""},
			0,
			false,
			"",
		},
		{
			"two lines in one write",
			new(strings.Builder),
			[]string{"a\nb\n"},
			4,
			false,
			"a\nb\n",
		},
		{
			"three lines in one write",
			new(strings.Builder),
			[]string{"a\nb\nc\n"},
			6,
			false,
			"a\nb\nc\n",
		},
		{
			"partial line without newline",
			new(strings.Builder),
			[]string{"abc"},
			3,
			false,
			"abc",
		},
		{
			"partial line completed in next write",
			new(strings.Builder),
			[]string{"he", "llo\n"},
			6,
			false,
			"hello\n",
		},
		{
			"partial then newline",
			new(strings.Builder),
			[]string{"hello", "\n"},
			6,
			false,
			"hello\n",
		},
		{
			"two separate calls, two lines",
			new(strings.Builder),
			[]string{"a\n", "b\n"},
			4,
			false,
			"a\nb\n",
		},
		{
			"empty lines",
			new(strings.Builder),
			[]string{"\n\n"},
			2,
			false,
			"\n\n",
		},
		{
			"three calls: partial + partial + newline",
			new(strings.Builder),
			[]string{"a", "b", "c\n"},
			4,
			false,
			"abc\n",
		},
		{
			"multi-chunk with newline in last",
			new(strings.Builder),
			[]string{"hello\nwor", "ld\n"},
			12,
			false,
			"hello\nworld\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			to := NewTimestampWriter(tt.w, tsFmt)
			var got int
			for _, s := range tt.chunks {
				n, err := to.Write([]byte(s))
				got += n
				if err != nil {
					if !tt.wantErr {
						t.Fatalf("Write() unexpected error: %v", err)
					}
				}
			}
			if tt.wantErr {
				t.Fatal("Write() succeeded unexpectedly")
			}
			if got != tt.want {
				t.Errorf("Write() returned %d bytes, want %d", got, tt.want)
			}
			gotContent := stripTS(tt.w.String())
			if gotContent != tt.wantContent {
				t.Errorf("Write() content = %q, want %q", gotContent, tt.wantContent)
			}
		})
	}
}

type errWriter struct {
	buf strings.Builder
	n   int
	err error
}

func (w *errWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		w.buf.Write(p[:w.n])
		return w.n, w.err
	}
	return w.buf.Write(p)
}

func TestTimestampWriter_WriteError(t *testing.T) {
	want := "hello\n"
	wantErr := errors.New("write failed")

	w := &errWriter{n: 10, err: wantErr}
	to := NewTimestampWriter(w, tsFmt)

	n, err := to.Write([]byte(want))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Write() error = %v, want %v", err, wantErr)
	}

	w.err = nil
	_, err = to.Write([]byte(want[n:]))
	if err != nil {
		t.Fatalf("Write() unexpected error: %v", err)
	}

	out := stripTS(w.buf.String())
	if out != want {
		t.Fatalf("Write() out = %q, want %q", out, want)
	}
}

func TestSafeTimestampLineWrite(t *testing.T) {
	const (
		goroutines = 8
		linesPerG  = 200
	)

	var buf strings.Builder
	sw := NewSafeWriter(&buf)

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()

			lw := NewLineWriter(sw)
			defer lw.Flush()

			tw := NewTimestampWriter(lw, tsFmt)

			for l := 0; l < linesPerG; l++ {
				line := fmt.Sprintf("g%d-l%d\n", g, l)
				if l == linesPerG-1 && g&1 == 1 {
					line = line[:len(line)-1] // trim last \n
				}
				if _, err := tw.Write([]byte(line)); err != nil {
					t.Errorf("Write() error: %v", err)
					return
				}
			}
		}(g)
	}

	wg.Wait()

	got := buf.String()
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	want := goroutines * linesPerG
	if len(lines) != want {
		t.Fatalf("got %d lines, want %d", len(lines), want)
	}

	// Каждая строка должна быть ровно `ts + содержимое` —
	// это проверяет, что Write полной строки атомарен под мьютексом.
	lineRe := regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} (g\d+-l\d+)$`)
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		m := lineRe.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("malformed line (interleaved/corrupted?): %q", line)
		}
		if _, ok := seen[m[1]]; ok {
			t.Fatalf("duplicate content: %q", m[1])
		}
		seen[m[1]] = struct{}{}
	}
}
