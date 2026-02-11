package agent

import (
	"bytes"
	"strconv"
	"strings"
)

var httpMethods = map[string]struct{}{
	"GET":     {},
	"POST":    {},
	"PUT":     {},
	"DELETE":  {},
	"PATCH":   {},
	"HEAD":    {},
	"OPTIONS": {},
}

func ParseRequestLine(data []byte) (string, string, bool) {
	line := firstLine(data)
	fields := bytes.Fields(line)
	if len(fields) < 2 {
		return "", "", false
	}

	method := strings.ToUpper(string(fields[0]))
	if _, ok := httpMethods[method]; !ok {
		return "", "", false
	}

	path := string(fields[1])
	return method, path, true
}

func ParseResponseLine(data []byte) (uint32, bool) {
	line := firstLine(data)
	fields := bytes.Fields(line)
	if len(fields) < 2 {
		return 0, false
	}

	if !bytes.HasPrefix(fields[0], []byte("HTTP/")) {
		return 0, false
	}

	status, err := strconv.Atoi(string(fields[1]))
	if err != nil || status < 0 {
		return 0, false
	}

	return uint32(status), true
}

func firstLine(data []byte) []byte {
	if idx := bytes.IndexByte(data, '\n'); idx >= 0 {
		return bytes.TrimSpace(data[:idx])
	}
	return bytes.TrimSpace(data)
}
