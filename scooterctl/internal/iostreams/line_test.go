package iostreams

import (
	"fmt"
	"strings"
	"testing"
)

func TestLineWriter_Flush(t *testing.T) {
	tests := []struct {
		name    string
		chunks  []string
		flushes int // сколько раз вызвать Flush подряд
		want    string
	}{
		{
			"empty flush",
			[]string{},
			1,
			"",
		},
		{
			"full line",
			[]string{"hello\n"},
			1,
			"hello\n",
		},
		{
			"partial line",
			[]string{"hello"},
			1,
			"hello\n",
		},
		{
			"few chunks",
			[]string{"he", "llo", "!"},
			1,
			"hello!\n",
		},
		{
			"few lines",
			[]string{"he", "llo\n", "wo"},
			1,
			"hello\nwo\n",
		},
		{
			"partial, then double flush",
			[]string{"hello"},
			2,
			"hello\n",
		},
		{
			"full line, then double flush",
			[]string{"test\n"},
			2,
			"test\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf strings.Builder
			lw := NewLineWriter(&buf)

			for _, chunk := range tt.chunks {
				if _, err := lw.Write([]byte(chunk)); err != nil {
					t.Fatalf("Write() error: %v", err)
				}
			}

			for i := 0; i < tt.flushes; i++ {
				if err := lw.Flush(); err != nil {
					t.Fatalf("Flush() #%d error = %v", i+1, err)
				}
			}

			if buf.String() != tt.want {
				t.Errorf("content = %q, want %q", buf.String(), tt.want)
			}
		})
	}
}

func TestLineWriter_WriteError(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		n       int
		err     error
		wantOut string
		wantErr bool
	}{
		{
			"success",
			"hello world\n",
			256,
			nil,
			"hello world\n",
			false,
		},
		{
			"underlying error",
			"hello world\n",
			256,
			fmt.Errorf("underlying error"),
			"",
			true,
		},
		{
			"short write",
			"hello world\n",
			5,
			nil,
			"",
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ew := &errWriter{n: tt.n, err: tt.err}
			lw := NewLineWriter(ew)

			_, err := lw.Write([]byte(tt.input))

			if (err != nil) != tt.wantErr {
				t.Errorf("Write() error = %v, want %v", err, tt.wantErr)
			}

			if err == nil {
				if out := ew.buf.String(); out != tt.wantOut {
					t.Errorf("Write() out = %q, want %q", out, tt.wantOut)
				}
			}
		})
	}
}

func TestLineWriter_FlushError(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		n       int
		err     error
		wantOut string
		wantErr bool
	}{
		{
			"success",
			"hello world",
			256,
			nil,
			"hello world\n",
			false,
		},
		{
			"underlying error",
			"hello world",
			256,
			fmt.Errorf("underlying error"),
			"",
			true,
		},
		{
			"short write",
			"hello world",
			5,
			nil,
			"",
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ew := &errWriter{n: tt.n, err: tt.err}
			lw := NewLineWriter(ew)

			_, err := lw.Write([]byte(tt.input))

			if err != nil {
				t.Fatalf("Write() unexpected error: %v", err)
			}

			if ew.buf.String() != "" {
				t.Fatal("Write() flush partial line")
			}

			err = lw.Flush()

			if (err != nil) != tt.wantErr {
				t.Errorf("Flush() error = %v, want %v", err, tt.wantErr)
			}

			if err == nil {
				if out := ew.buf.String(); out != tt.wantOut {
					t.Errorf("Flush() out = %q, want %q", out, tt.wantOut)
				}
			}
		})
	}
}
