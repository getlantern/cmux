package cmux

import (
	"testing"

	"github.com/xtaci/smux"
)

func TestSmux(t *testing.T) {
	RunAllProtocolTests(defaultProtocol, t)
	config := smux.DefaultConfig()
	config.Version = 2
	proto := NewSmuxProtocol(config)
	RunAllProtocolTests(proto, t)
}
