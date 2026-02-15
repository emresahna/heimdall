package models

import "time"

type Direction uint8

const (
	DirectionUnknown  Direction = 0
	DirectionRequest  Direction = 1
	DirectionResponse Direction = 2
)

type Event struct {
	Timestamp time.Time
	Pid       uint32
	Tid       uint32
	Fd        int32
	CgroupID  uint64
	Direction Direction
	Data      []byte
}

type RawEvent struct {
	TsNs      uint64
	CgroupID  uint64
	Pid       uint32
	Tid       uint32
	Fd        int32
	DataLen   uint32
	EventType uint8
	_         [3]byte
	Data      [128]byte
}
