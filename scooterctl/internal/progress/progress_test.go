package progress

import (
	"bytes"
	"encoding/base64"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/pkg/jsonmessage"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func consumeProgress(reader io.Reader, out io.Writer) error {
	r := NewTraceRender(out, slog.New(slog.DiscardHandler))
	return jsonmessage.DisplayJSONMessagesStream(reader, out, 0, false, r.AuxCallback)
}

// =============================================================================
// BuilderV1 regression — the whole point of these tests is to prove that
// swapping the hand-rolled decoder for jsonmessage.DisplayJSONMessagesStream
// doesn't break the classic docker builder output.
// =============================================================================

func buildV1Stream() string {
	msgs := []string{
		`{"stream":"Step 1/3 : FROM alpine:3.19\n"}`,
		`{"status":"Pulling from library/alpine","id":"3.19"}`,
		`{"status":"Pulling fs layer","id":"a1b2c3"}`,
		`{"status":"Pull complete","id":"a1b2c3"}`,
		`{"status":"Digest: sha256:abc"}`,
		`{"status":"Status: Downloaded newer image for alpine:3.19"}`,
		`{"stream":" ---> 1111111111\n"}`,
		`{"stream":"Step 2/3 : RUN echo hello\n"}`,
		`{"stream":"hello\n"}`,
		`{"stream":" ---> 2222222222\n"}`,
		`{"stream":"Step 3/3 : CMD [\"sh\"]\n"}`,
		`{"stream":" ---> 3333333333\n"}`,
		`{"stream":"Successfully built 3333333333\n"}`,
		`{"stream":"Successfully tagged example:latest\n"}`,
	}
	return strings.Join(msgs, "\n") + "\n"
}

func TestConsumeProgress_V1(t *testing.T) {
	var out bytes.Buffer
	if err := consumeProgress(strings.NewReader(buildV1Stream()), &out); err != nil {
		t.Fatalf("consumeProgress returned error: %v", err)
	}
	got := out.String()

	// "stream" fields are copied verbatim (they embed their own newlines).
	for _, want := range []string{
		"Step 1/3 : FROM alpine:3.19",
		" ---> 1111111111",
		"Step 2/3 : RUN echo hello",
		"hello",
		"Successfully built 3333333333",
		"Successfully tagged example:latest",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("V1 stream output missing %q\nfull output:\n%s", want, got)
		}
	}

	// "status" fields are still printed.
	for _, want := range []string{
		"Pulling from library/alpine",
		"Pull complete",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("V1 status %q missing from output:\n%s", want, got)
		}
	}
}

func TestConsumeProgress_V1_Error(t *testing.T) {
	stream := `{"errorDetail":{"message":"no such image"},"error":"no such image"}` + "\n"
	var out bytes.Buffer
	err := consumeProgress(strings.NewReader(stream), &out)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no such image") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestConsumeProgress_V1_DifferentLayersNotCollapsed фиксирует изменение
// поведения относительно старого самописного декодера. Старый код
// дедуплицировал только по event.Status (игнорируя event.ID), поэтому три
// слоя с одинаковым текстом статуса он печатал бы как одну строку. Новый
// код использует jsonmessage, который включает ID в строку ("a: ...",
// "b: ...", "c: ..."), поэтому каждый слой печатается отдельно.
//
// Тест страхует от случайного возврата к старому поведению (1 строка
// вместо 3).
func TestConsumeProgress_V1_DifferentLayersNotCollapsed(t *testing.T) {
	stream := strings.Join([]string{
		`{"status":"Pulling fs layer","id":"a"}`,
		`{"status":"Pulling fs layer","id":"b"}`,
		`{"status":"Pulling fs layer","id":"c"}`,
	}, "\n") + "\n"

	var out bytes.Buffer
	if err := consumeProgress(strings.NewReader(stream), &out); err != nil {
		t.Fatalf("consumeProgress: %v", err)
	}
	if n := strings.Count(out.String(), "Pulling fs layer"); n != 3 {
		t.Fatalf("want 3 lines (one per layer), got %d:\n%s", n, out.String())
	}
}

func TestConsumeProgress_V1_EmptyInput(t *testing.T) {
	var out bytes.Buffer
	if err := consumeProgress(strings.NewReader(""), &out); err != nil {
		t.Fatalf("consumeProgress on empty input: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected empty output, got: %q", out.String())
	}
}

// =============================================================================
// BuildKit trace tests
// =============================================================================

// --- protobuf wire helpers --------------------------------------------------

func appendStringField(b []byte, num protowire.Number, s string) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendString(b, s)
}

func appendBytesField(b []byte, num protowire.Number, v []byte) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendBytes(b, v)
}

func appendVarintField(b []byte, num protowire.Number, v uint64) []byte {
	b = protowire.AppendTag(b, num, protowire.VarintType)
	return protowire.AppendVarint(b, v)
}

func appendTimestampField(b []byte, num protowire.Number, t time.Time) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	v, _ := proto.Marshal(timestamppb.New(t))
	return protowire.AppendBytes(b, v)
}

func buildStatusResponse(vertices, logs, warnings [][]byte) []byte {
	var out []byte
	for _, v := range vertices {
		out = appendBytesField(out, 1, v)
	}
	for _, l := range logs {
		out = appendBytesField(out, 3, l)
	}
	for _, w := range warnings {
		out = appendBytesField(out, 4, w)
	}
	return out
}

func buildVertex(digest, name string, cached, completed bool, errMsg string) []byte {
	var v []byte
	v = appendStringField(v, 1, digest)
	v = appendStringField(v, 3, name)
	if cached {
		v = appendVarintField(v, 4, 1)
	}
	started := time.Date(1914, 07, 28, 0, 0, 0, 0, time.UTC)
	v = appendTimestampField(v, 5, started)
	if completed {
		v = appendTimestampField(v, 6, started.Add(time.Second))
	}
	if errMsg != "" {
		v = appendStringField(v, 7, errMsg)
	}
	return v
}

func buildVertexLog(vertex string, timestamp time.Time, stream int, msg string) []byte {
	var v []byte
	v = appendStringField(v, 1, vertex)
	v = appendTimestampField(v, 2, time.Now())
	v = appendVarintField(v, 3, uint64(stream))
	v = appendBytesField(v, 4, []byte(msg))
	return v
}

func buildVertexWarning(vertex, short, detail string) []byte {
	var w []byte
	w = appendStringField(w, 1, vertex)
	w = appendStringField(w, 3, short)
	w = appendStringField(w, 4, detail)
	return w
}

func wrapAuxTrace(resp []byte) string {
	return `{"id":"moby.buildkit.trace","aux":"` +
		base64.StdEncoding.EncodeToString(resp) + `"}` + "\n"
}

// --- BuildKit trace behaviour ----------------------------------------------

func TestConsumeProgress_BuildKit_DoneVertex(t *testing.T) {
	vertex := buildVertex("sha256:deadbeefcafe0123456789abcdef", "[1/2] RUN echo hi", false, true, "")
	stream := wrapAuxTrace(buildStatusResponse([][]byte{vertex}, nil, nil))

	var out bytes.Buffer
	if err := consumeProgress(strings.NewReader(stream), &out); err != nil {
		t.Fatalf("consumeProgress: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "DONE") {
		t.Errorf("expected DONE marker, got:\n%s", got)
	}
	if !strings.Contains(got, "[1/2] RUN echo hi") {
		t.Errorf("expected vertex name, got:\n%s", got)
	}
}

func TestConsumeProgress_BuildKit_CachedVertex(t *testing.T) {
	vertex := buildVertex("sha256:0011223344556677", "[2/2] COPY . .", true, true, "")
	stream := wrapAuxTrace(buildStatusResponse([][]byte{vertex}, nil, nil))

	var out bytes.Buffer
	if err := consumeProgress(strings.NewReader(stream), &out); err != nil {
		t.Fatalf("consumeProgress: %v", err)
	}
	if !strings.Contains(out.String(), "CACHED") {
		t.Errorf("expected CACHED marker, got:\n%s", out.String())
	}
}

func TestConsumeProgress_BuildKit_Error(t *testing.T) {
	vertex := buildVertex("sha256:0011223344556677", "[2/2] COPY . .", false, true, "copy-failed")
	stream := wrapAuxTrace(buildStatusResponse([][]byte{vertex}, nil, nil)) +
		`{"errorDetail":{"message":"copy-failed"}}` + "\n"
	var out bytes.Buffer
	if err := consumeProgress(strings.NewReader(stream), &out); err == nil {
		t.Errorf("expected error")
	}
	got := out.String()
	if !strings.Contains(got, "ERROR") {
		t.Errorf("expected ERROR marker, got:\n%s", got)
	}
	if !strings.Contains(got, "copy-failed") {
		t.Errorf("expected error message, got:\n%s", got)
	}
}

func TestConsumeProgress_BuildKit_InProgressNotReported(t *testing.T) {
	vertex := buildVertex("sha256:aaaaaaaaaaaaaaaa", "still running", false, false, "")
	stream := wrapAuxTrace(buildStatusResponse([][]byte{vertex}, nil, nil))

	var out bytes.Buffer
	if err := consumeProgress(strings.NewReader(stream), &out); err != nil {
		t.Fatalf("consumeProgress: %v", err)
	}
	got := strings.TrimSuffix(out.String(), "\n")
	if len(strings.Split(got, "\n")) > 1 || strings.Contains(got, "DONE") || strings.Contains(got, "CACHED") || strings.Contains(got, "ERROR") {
		t.Errorf("in-progress vertex must not be reported, got:\n%s", out.String())
	}
}

func TestConsumeProgress_BuildKit_StateAcrossMessages(t *testing.T) {
	const digest = "sha256:1234567890abcdef"
	started := buildVertex(digest, "[1/1] RUN long step", false, false, "")
	completed := buildVertex(digest, "[1/1] RUN long step", false, true, "")

	stream := wrapAuxTrace(buildStatusResponse([][]byte{started}, nil, nil)) +
		wrapAuxTrace(buildStatusResponse([][]byte{completed}, nil, nil))

	var out bytes.Buffer
	if err := consumeProgress(strings.NewReader(stream), &out); err != nil {
		t.Fatalf("consumeProgress: %v", err)
	}
	if n := strings.Count(out.String(), "DONE"); n != 1 {
		t.Errorf("expected exactly one report line, got %d:\n%s", n, out.String())
	}
}

func TestConsumeProgress_BuildKit_VertexLog(t *testing.T) {
	now := time.Now()
	logMsg := buildVertexLog("sha256:0011223344556677", now, 1, "compiling pkg/foo\n")
	stream := wrapAuxTrace(buildStatusResponse(nil, [][]byte{logMsg}, nil))

	var out bytes.Buffer
	if err := consumeProgress(strings.NewReader(stream), &out); err != nil {
		t.Fatalf("consumeProgress: %v", err)
	}
	if !strings.Contains(out.String(), "compiling pkg/foo") {
		t.Errorf("expected log line, got:\n%s", out.String())
	}
}

func TestConsumeProgress_BuildKit_Warning(t *testing.T) {
	warning := buildVertexWarning("sha256:0011223344556677", "legacy ENV syntax", "use ENV k=v instead")
	stream := wrapAuxTrace(buildStatusResponse(nil, nil, [][]byte{warning}))

	var out bytes.Buffer
	if err := consumeProgress(strings.NewReader(stream), &out); err != nil {
		t.Fatalf("consumeProgress: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "legacy ENV syntax") || !strings.Contains(got, "use ENV k=v instead") {
		t.Errorf("expected short and detail, got:\n%s", got)
	}
}

func TestConsumeProgress_BuildKit_ImageID(t *testing.T) {
	const digest = "sha256:aa1bba080939e9d447ab2f6e441602efd91b362cd838e0ed13c8c46c776a872a"
	stream := `{"id":"moby.image.id","aux":{"ID":"` + digest + `"}}` + "\n"

	var out bytes.Buffer
	if err := consumeProgress(strings.NewReader(stream), &out); err != nil {
		t.Fatalf("consumeProgress: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Successfully built") {
		t.Errorf("expected 'Successfully built', got:\n%s", got)
	}
	if !strings.Contains(got, "aa1bba080939e9d447ab2f6e441602efd91b362cd838e0ed13c8c46c776a872a") {
		t.Errorf("expected full digest body, got:\n%s", got)
	}
}

func TestConsumeProgress_BuildKit_MalformedAuxIgnored(t *testing.T) {
	stream := `{"id":"moby.buildkit.trace","aux":"not!valid!base64"}` + "\n" +
		`{"stream":"after-bad-aux\n"}`

	var out bytes.Buffer
	if err := consumeProgress(strings.NewReader(stream), &out); err != nil {
		t.Fatalf("consumeProgress must tolerate malformed aux, got error: %v", err)
	}
	if !strings.Contains(out.String(), "after-bad-aux") {
		t.Errorf("stream after malformed aux was lost, got:\n%s", out.String())
	}
}

func TestConsumeProgress_BuildKit_RealFixture(t *testing.T) {
	lines := []string{
		// vertex start
		`{"id":"moby.buildkit.trace","aux":"Cm8KR3NoYTI1NjpmMjY0NDQ4MjFjNGU0YTI1ZDdjZWU2M2JiMzk0OTUyOTQ4NjBjYzFiYzAyZTUwOWU5ZmMwMGUzYWNjZWQ5NDIwGiRbaW50ZXJuYWxdIGxvYWQgcmVtb3RlIGJ1aWxkIGNvbnRleHQ="}`,
		// same vertex, completed
		`{"id":"moby.buildkit.trace","aux":"CokBCkdzaGEyNTY6ZjI2NDQ0ODIxYzRlNGEyNWQ3Y2VlNjNiYjM5NDk1Mjk0ODYwY2MxYmMwMmU1MDllOWZjMDBlM2FjY2VkOTQyMBokW2ludGVybmFsXSBsb2FkIHJlbW90ZSBidWlsZCBjb250ZXh0KgsIvJHd1wYQ+Pe9AzILCLyR3dcGENDtxkA="}`,
		// final image id
		`{"id":"moby.image.id","aux":{"ID":"sha256:aa1bba080939e9d447ab2f6e441602efd91b362cd838e0ed13c8c46c776a872a"}}`,
	}
	stream := strings.Join(lines, "\n") + "\n"

	var out bytes.Buffer
	if err := consumeProgress(strings.NewReader(stream), &out); err != nil {
		t.Fatalf("consumeProgress: %v", err)
	}
	got := out.String()
	var failure bool
	if !strings.Contains(got, "[internal] load remote build context") {
		t.Error("expected head '[internal] ...'")
		failure = true
	}
	if !strings.Contains(got, "DONE") {
		t.Error("expected report DONE")
		failure = true
	}
	if !strings.Contains(got, "Successfully built") {
		t.Errorf("expected 'Successfully built'")
		failure = true
	}
	if failure {
		t.Logf("got:\n%s", got)
	}
}
