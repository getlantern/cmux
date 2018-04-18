// Package cmux provides multiplexing over net.Conns using smux and adhering
// to standard net package interfaces.
package cmux

import (
	"net"
	"sync"

	"github.com/getlantern/golog"
)

var (
	log               = golog.LoggerFor("cmux")
	defaultBufferSize = 4194304
)

type Conn interface {
	net.Conn
	Closed() bool
}

type cmconn struct {
	net.Conn
	cs      *connAndSession
	onClose func()
	closed  bool
	mx      sync.Mutex
}

func (c *cmconn) Close() error {
	c.mx.Lock()
	defer c.mx.Unlock()
	if c.closed {
		return nil
	}
	err := c.Conn.Close()
	c.onClose()
	c.closed = true
	return err
}

func (c *cmconn) Closed() bool {
	c.mx.Lock()
	closed := c.closed
	c.mx.Unlock()
	return closed || c.cs.session.IsClosed()
}
