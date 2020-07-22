package cmux

import (
	"testing"
)

func TestSmux(t *testing.T) {
	RunAllProtocolTests(defaultProtocol, t)
	proto := NewSmuxProtocol()
	proto.Version = 2
	RunAllProtocolTests(proto, t)
}
