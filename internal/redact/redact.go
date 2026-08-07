package redact

type Redactor interface {
	Redact([]byte) []byte
}
