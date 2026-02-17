package collector

import (
	"bytes"
	"encoding/binary"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emresahna/heimdall/internal/models"
)

func resetParserState() {
	bootTime = time.Time{}
	once = sync.Once{}
	initErr = nil
	maxEventData = 128
}

func encodeRawEvent(t *testing.T, evt rawEvent) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.LittleEndian, evt); err != nil {
		t.Fatalf("binary.Write failed: %v", err)
	}
	return buf.Bytes()
}

func TestParseEventSuccess(t *testing.T) {
	resetParserState()

	var raw rawEvent
	raw.TsNs = uint64((45 * time.Second).Nanoseconds())
	raw.CgroupID = 99
	raw.Pid = 1234
	raw.Tid = 2345
	raw.Fd = 8
	raw.DataLen = 7
	raw.EventType = uint8(models.DirectionRequest)
	copy(raw.Data[:], []byte("GET /x\x00"))

	evt, err := parseEvent(encodeRawEvent(t, raw))
	if err != nil {
		t.Fatalf("parseEvent returned error: %v", err)
	}

	if evt.CgroupID != raw.CgroupID || evt.Pid != raw.Pid || evt.Tid != raw.Tid ||
		evt.Fd != raw.Fd {
		t.Fatalf("unexpected metadata parsed: %+v", evt)
	}
	if evt.Direction != models.DirectionRequest {
		t.Fatalf("unexpected direction: %v", evt.Direction)
	}
	if string(evt.Data) != "GET /x" {
		t.Fatalf("unexpected data: %q", string(evt.Data))
	}

	wantTimestamp := bootTime.Add(45 * time.Second)
	if !evt.Timestamp.Equal(wantTimestamp) {
		t.Fatalf("unexpected timestamp: got=%s want=%s", evt.Timestamp, wantTimestamp)
	}
}

func TestParseEventClampsDataLength(t *testing.T) {
	resetParserState()

	var raw rawEvent
	raw.TsNs = uint64((2 * time.Second).Nanoseconds())
	raw.DataLen = 512
	copy(raw.Data[:], []byte(strings.Repeat("a", 128)))

	evt, err := parseEvent(encodeRawEvent(t, raw))
	if err != nil {
		t.Fatalf("parseEvent returned error: %v", err)
	}

	if len(evt.Data) != 128 {
		t.Fatalf("unexpected data length: got=%d want=128", len(evt.Data))
	}
}

func TestParseEventBinaryReadError(t *testing.T) {
	resetParserState()

	_, err := parseEvent([]byte{1, 2, 3, 4})
	if err == nil {
		t.Fatalf("expected error for malformed payload")
	}
	if !strings.Contains(err.Error(), "binary read failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}
