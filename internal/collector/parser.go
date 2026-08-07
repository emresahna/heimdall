package collector

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/emresahna/heimdall/internal/models"
	"golang.org/x/sys/unix"
)

var (
	bootTime     time.Time
	once         sync.Once
	initErr      error
	maxEventData = 128
)

type rawEvent struct {
	TsNs      uint64
	CgroupID  uint64
	Pid       uint32
	Tid       uint32
	Fd        int32
	DataLen   uint32
	EventType uint8
	_         [3]byte
	Seqno     uint32
	Data      [128]byte
}

func parseEvent(raw []byte) (models.Event, error) {
	once.Do(func() {
		var ts unix.Timespec
		if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
			initErr = err
			return
		}
		bootTime = time.Now().Add(time.Duration(-ts.Nano()))
	})

	if initErr != nil {
		return models.Event{}, fmt.Errorf("parser not initialized: %w", initErr)
	}

	var rawEvt rawEvent
	if err := binary.Read(bytes.NewReader(raw), binary.LittleEndian, &rawEvt); err != nil {
		return models.Event{}, fmt.Errorf("binary read failed: %w", err)
	}

	eventTime := bootTime.Add(time.Duration(rawEvt.TsNs))

	realDataLen := min(int(rawEvt.DataLen), maxEventData)

	evt := models.Event{
		Timestamp: eventTime,
		CgroupID:  rawEvt.CgroupID,
		Pid:       rawEvt.Pid,
		Tid:       rawEvt.Tid,
		Fd:        rawEvt.Fd,
		Seqno:     rawEvt.Seqno,
		Direction: models.Direction(rawEvt.EventType),
		Data:      bytes.TrimRight(rawEvt.Data[:realDataLen], "\x00"),
	}

	return evt, nil
}
