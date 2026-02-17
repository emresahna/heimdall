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
