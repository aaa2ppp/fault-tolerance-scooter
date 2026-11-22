package progress

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/f0rmul11/fault-tolerance-scooter/scooterctl/internal/iostreams"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type prefixLineWriter struct {
	lw *iostreams.LineWriter
	pw *iostreams.PrefixWriter
}

func newPrefixLineWriter(out io.Writer, prefix string) *prefixLineWriter {
	lw := iostreams.NewLineWriter(out)
	pw := iostreams.NewPrefixWriter(lw, prefix)
	return &prefixLineWriter{lw: lw, pw: pw}
}

func (w *prefixLineWriter) Prefix(prefix string)        { w.pw.Prefix(prefix) }
func (w *prefixLineWriter) Write(b []byte) (int, error) { return w.pw.Write(b) }
func (w *prefixLineWriter) Flush() error                { return w.lw.Flush() }

type vertexState struct {
	id          int
	name        string
	started     time.Time
	completed   time.Time
	errorMsg    string
	out         *iostreams.PrefixWriter
	log         [2]*prefixLineWriter
	detail      *prefixLineWriter
	cached      bool
	headPrinted bool
	reported    bool
}

func newVertexState(id int, out io.Writer) *vertexState {
	out2 := iostreams.NewPrefixWriter(out, fmt.Sprintf("#%-2d ", id))
	vs := &vertexState{
		id:  id,
		out: out2,
		log: [2]*prefixLineWriter{
			newPrefixLineWriter(out2, " 0.000 1: "),
			newPrefixLineWriter(out2, " 0.000 2: "),
		},
		detail: newPrefixLineWriter(out2, "    "),
	}
	return vs
}

func (vs *vertexState) flushLog() error {
	return errors.Join(
		vs.log[0].Flush(),
		vs.log[1].Flush(),
	)
}

type TraceRender struct {
	out       io.Writer
	logger    *slog.Logger
	inception time.Time
	lastID    int
	state     map[string]*vertexState
}

func NewTraceRender(out io.Writer, logger *slog.Logger) *TraceRender {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &TraceRender{
		out:    out,
		logger: logger,
		state:  map[string]*vertexState{},
	}
}

func (r *TraceRender) getVertex(digest string) *vertexState {
	st := r.state[digest]
	if st == nil {
		r.lastID++
		st = newVertexState(r.lastID, r.out)
		r.state[digest] = st
	}
	return st
}

func (r *TraceRender) begin(t time.Time) {
	if t.IsZero() || !r.inception.IsZero() {
		return
	}
	r.inception = t
}

func (r *TraceRender) offset(t time.Time) time.Duration {
	if t.IsZero() || r.inception.IsZero() {
		return 0
	}
	return t.Sub(r.inception)
}

func (r *TraceRender) AuxCallback(msg jsonmessage.JSONMessage) {
	switch msg.ID {
	case "moby.buildkit.trace":
		if err := r.handleBuildKitTrace(msg.Aux); err != nil {
			r.logger.Error("handle aux failed", "msg", msg, "handler", "handleBuildKitTrace", "error", err)
		}
	case "moby.image.id":
		if err := r.handleImageID(msg.Aux); err != nil {
			r.logger.Error("handle aux failed", "msg", msg, "handler", "handleImageID", "error", err)
		}
	case "":
		if err := r.handleImageID(msg.Aux); err != nil {
			r.logger.Warn("unknown aux id", "msg", msg)
		}
	default:
		r.logger.Warn("unknown aux id", "msg", msg)
	}
}

// handleBuildKitTrace разбирает поток StatusResponse от BuildKit.
//
//	{"id":"moby.buildkit.trace","aux":"Cm8KR3No...="}
func (r *TraceRender) handleBuildKitTrace(aux *json.RawMessage) error {
	var b64 string
	if err := json.Unmarshal(*aux, &b64); err != nil {
		return fmt.Errorf("json decode aux: %w", err)
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("base64 decode aux: %w", err)
	}
	return r.handleStatusResponse(data)
}

// handleImageID обрабатывает финальное сообщение с ID образа.
//
//	{"id":"moby.image.id","aux":{"ID":"sha256:aa1bba080..."}}
func (r *TraceRender) handleImageID(aux *json.RawMessage) error {
	var result struct {
		ID string `json:"ID"`
	}
	if err := json.Unmarshal(*aux, &result); err != nil {
		return fmt.Errorf("json decode aux: %v", err)
	}
	if result.ID == "" {
		return fmt.Errorf("missing image ID")
	}
	fmt.Fprintf(r.out, "Successfully built %s\n", result.ID)
	return nil
}

// handleStatusResponse
//
// see: https://github.com/moby/buildkit/blob/master/api/services/control/control.proto
//
//	message StatusResponse {
//		repeated Vertex vertexes = 1;
//		repeated VertexStatus statuses = 2;
//		repeated VertexLog logs = 3;
//		repeated VertexWarning warnings = 4;
//	}
func (r *TraceRender) handleStatusResponse(data []byte) error {
	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return protowire.ParseError(n)
		}
		data = data[n:]

		if typ != protowire.BytesType {
			m := protowire.ConsumeFieldValue(num, typ, data)
			if m < 0 {
				return protowire.ParseError(m)
			}
			data = data[m:]
			continue
		}

		val, m := protowire.ConsumeBytes(data)
		if m < 0 {
			return protowire.ParseError(m)
		}
		data = data[m:]

		switch num {
		case 1:
			if err := r.handleVertex(val); err != nil {
				return err
			}
		case 2:
			if err := r.handleVertexStatus(val); err != nil {
				return err
			}
		case 3:
			if err := r.handleVertexLog(val); err != nil {
				return err
			}
		case 4:
			if err := r.handleVertexWarning(val); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseTimestamp(val []byte) (time.Time, error) {
	var ts timestamppb.Timestamp
	if err := proto.Unmarshal(val, &ts); err != nil {
		return time.Time{}, err
	}
	if err := ts.CheckValid(); err != nil {
		return time.Time{}, err
	}
	return ts.AsTime(), nil
}

// handleVertex
//
//	message Vertex {
//		string digest = 1;
//		repeated string inputs = 2;
//		string name = 3;
//		bool cached = 4;
//		google.protobuf.Timestamp started = 5;
//		google.protobuf.Timestamp completed = 6;
//		string error = 7; // typed errors?
//		pb.ProgressGroup progressGroup = 8;
//	}
func (r *TraceRender) handleVertex(data []byte) error {
	var digest, name, errorMsg string
	var cached bool
	var started, completed time.Time

	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return protowire.ParseError(n)
		}
		data = data[n:]
		switch typ {
		case protowire.BytesType:
			val, m := protowire.ConsumeBytes(data)
			if m < 0 {
				return protowire.ParseError(m)
			}
			data = data[m:]
			switch num {
			case 1:
				digest = string(val)
			case 3:
				name = string(val)
			case 5:
				var err error
				started, err = parseTimestamp(val)
				if err != nil {
					return fmt.Errorf("started: %w", err)
				}
			case 6:
				var err error
				completed, err = parseTimestamp(val)
				if err != nil {
					return fmt.Errorf("completed: %w", err)
				}
			case 7:
				errorMsg = string(val)
			}
		case protowire.VarintType:
			val, m := protowire.ConsumeVarint(data)
			if m < 0 {
				return protowire.ParseError(m)
			}
			data = data[m:]
			switch num {
			case 4:
				cached = val != 0
			}
		default:
			m := protowire.ConsumeFieldValue(num, typ, data)
			if m < 0 {
				return protowire.ParseError(m)
			}
			data = data[m:]
		}
	}

	if digest == "" {
		r.logger.Warn("skip vertex with missing digest",
			"name", name,
			"started", started,
			"error", errorMsg,
		)
		return nil
	}

	r.begin(started)
	r.begin(completed)

	st := r.getVertex(digest)

	r.logger.Debug("vertex message received",
		"digest", shortDigest(digest),
		"name", name,
		"started", started,
		"completed", completed,
		"cached", cached,
		"error", errorMsg,
		"state_started", st.started,
		"state_completed", st.completed,
	)

	if name != "" && name != st.name {
		st.name = name
		st.headPrinted = false
	}
	if !started.IsZero() && st.started.IsZero() {
		st.started = started
		st.headPrinted = false
	}

	if !st.headPrinted && st.name != "" && !st.started.IsZero() {
		st.flushLog()
		offset := r.offset(st.started).Seconds()
		fmt.Fprintf(st.out, "%6.3f %s\n", offset, st.name)
		st.headPrinted = true
	}

	if cached && !st.cached {
		if !completed.IsZero() {
			st.completed = completed
		}
		st.cached = true
		st.reported = false
	}
	if errorMsg != "" && errorMsg != st.errorMsg {
		if !completed.IsZero() {
			st.completed = completed
		}
		st.errorMsg = errorMsg
		st.reported = false
	}
	if !completed.IsZero() && st.completed.IsZero() {
		st.completed = completed
		st.reported = false
	}

	if !st.reported && !st.started.IsZero() && (!st.completed.IsZero() || st.errorMsg != "") {
		st.flushLog()

		completed := st.completed
		if completed.IsZero() {
			completed = time.Now()
		}
		offset := r.offset(completed).Seconds()

		switch {
		case st.errorMsg != "":
			fmt.Fprintf(st.out, "%6.3f ERROR: %s\n", offset, st.errorMsg)
		case st.cached:
			fmt.Fprintf(st.out, "%6.3f CACHED\n", offset)
		default:
			fmt.Fprintf(st.out, "%6.3f DONE\n", offset)
		}
		st.reported = true
	}

	return nil
}

// handleVertexStatus
//
//	message VertexStatus {
//		string ID = 1;
//		string vertex = 2;
//		string name = 3;
//		int64 current = 4;
//		int64 total = 5;
//		google.protobuf.Timestamp timestamp = 6;
//		google.protobuf.Timestamp started = 7;
//		google.protobuf.Timestamp completed = 8;
//	}
func (r *TraceRender) handleVertexStatus(data []byte) error {
	// TODO: it necessary?
	_ = data
	return nil
}

func shortDigest(digest string) string {
	if _, tail, ok := strings.Cut(digest, ":"); ok {
		digest = tail
	}
	if len(digest) > 12 {
		digest = digest[:12]
	}
	return digest
}

// handleVertexLog
//
//	message VertexLog {
//		string vertex = 1;
//		google.protobuf.Timestamp timestamp = 2;
//		int64 stream = 3;
//		bytes msg = 4;
//	}
func (r *TraceRender) handleVertexLog(data []byte) error {
	var vertex, msg []byte
	var timestamp time.Time
	var stream int64

	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return protowire.ParseError(n)
		}
		data = data[n:]

		switch typ {
		case protowire.BytesType:
			val, m := protowire.ConsumeBytes(data)
			if m < 0 {
				return protowire.ParseError(m)
			}
			data = data[m:]

			switch num {
			case 1:
				vertex = val
			case 2:
				var err error
				timestamp, err = parseTimestamp(val)
				if err != nil {
					return fmt.Errorf("timestamp: %w", err)
				}
			case 4:
				msg = val
			}
		case protowire.VarintType:
			val, m := protowire.ConsumeVarint(data)
			if m < 0 {
				return protowire.ParseError(m)
			}
			data = data[m:]

			if num == 3 {
				stream = int64(val)
			}
		default:
			m := protowire.ConsumeFieldValue(num, typ, data)
			if m < 0 {
				return protowire.ParseError(m)
			}
			data = data[m:]
		}
	}

	if len(vertex) == 0 {
		r.logger.Warn("skip log with missing vertex",
			"timestamp", timestamp,
			"msg", msg,
		)
		return nil
	}

	if stream != 1 && stream != 2 {
		r.logger.Warn("use 1 instead of an invalid stream",
			"vertex", shortDigest(string(vertex)),
			"stream", stream,
		)
		stream = 1
	}

	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	r.begin(timestamp)

	st := r.getVertex(string(vertex))
	offset := r.offset(timestamp).Seconds()
	prefix := fmt.Sprintf("%6.3f %d: ", offset, stream)
	st.log[stream-1].Prefix(prefix)
	st.log[stream-1].Write(msg)

	return nil
}

// handleVertexWarning
//
//	message VertexWarning {
//		string vertex = 1;
//		int64 level = 2;
//		bytes short = 3;
//		repeated bytes detail = 4;
//		string url = 5;
//		pb.SourceInfo info = 6;
//		repeated pb.Range ranges = 7;
//	}
func (r *TraceRender) handleVertexWarning(data []byte) error {
	var vertex, short, url []byte
	var detail [][]byte

	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return protowire.ParseError(n)
		}
		data = data[n:]
		switch typ {
		case protowire.BytesType:
			val, m := protowire.ConsumeBytes(data)
			if m < 0 {
				return protowire.ParseError(m)
			}
			data = data[m:]
			switch num {
			case 1:
				vertex = val
			case 3:
				short = val
			case 4:
				detail = append(detail, val)
			case 5:
				url = val
			}
		default:
			m := protowire.ConsumeFieldValue(num, typ, data)
			if m < 0 {
				return protowire.ParseError(m)
			}
			data = data[m:]
		}
	}

	if len(vertex) == 0 {
		r.logger.Warn("skip warning with missing vertex",
			"short", short,
			"url", url,
		)
		return nil
	}

	st := r.getVertex(string(vertex))
	st.flushLog()

	if len(url) > 0 {
		fmt.Fprintf(r.out, "WARN: %s (%s)\n", short, url)
	} else {
		fmt.Fprintf(r.out, "WARN: %s\n", short)
	}
	for _, para := range detail {
		st.detail.Write(para)
		st.detail.Flush()
	}

	return nil
}
