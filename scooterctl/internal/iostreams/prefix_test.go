package iostreams

import (
	"strings"
	"testing"
)

func TestPrefixWriter_Write(t *testing.T) {
	const P = "PREFIX "
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
			P + "one\n",
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
			P + "a\n" + P + "b\n",
		},
		{
			"three lines in one write",
			new(strings.Builder),
			[]string{"a\nb\nc\n"},
			6,
			false,
			P + "a\n" + P + "b\n" + P + "c\n",
		},
		{
			"partial line without newline",
			new(strings.Builder),
			[]string{"abc"},
			3,
			false,
			P + "abc",
		},
		{
			"partial line completed in next write",
			new(strings.Builder),
			[]string{"he", "llo\n"},
			6,
			false,
			P + "hello\n",
		},
		{
			"partial then newline",
			new(strings.Builder),
			[]string{"hello", "\n"},
			6,
			false,
			P + "hello\n",
		},
		{
			"two separate calls, two lines",
			new(strings.Builder),
			[]string{"a\n", "b\n"},
			4,
			false,
			P + "a\n" + P + "b\n",
		},
		{
			"empty lines",
			new(strings.Builder),
			[]string{"\n\n"},
			2,
			false,
			P + "\n" + P + "\n",
		},
		{
			"three calls: partial + partial + newline",
			new(strings.Builder),
			[]string{"a", "b", "c\n"},
			4,
			false,
			P + "abc\n",
		},
		{
			"multi-chunk with newline in last",
			new(strings.Builder),
			[]string{"hello\nwor", "ld\n"},
			12,
			false,
			P + "hello\n" + P + "world\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			to := NewPrefixWriter(tt.w, P)
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
			gotContent := tt.w.String()
			if gotContent != tt.wantContent {
				t.Errorf("Write() content = %q, want %q", gotContent, tt.wantContent)
			}
		})
	}
}
