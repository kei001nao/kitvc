package ui

type ttyWriter interface {
	Write([]byte) (int, error)
	WriteString(string) (int, error)
	Sync() error
	Close() error
}

var stdTTY ttyWriter

func init() {
	stdTTY = openTTY()
}
