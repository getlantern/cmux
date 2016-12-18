// Package cmux provides multiplexing over net.Conns using smux and adhering
// to standard net package interfaces.
package cmux

import (
	"github.com/getlantern/golog"
	"github.com/whyrusleeping/yamux"
	"net"
	"sync/atomic"
	"time"
)

var (
	log = golog.LoggerFor("cmux")
)

type cmconn struct {
	wrapped net.Conn
	stream  *yamux.Stream
	onClose func()
	closed  int32
}

func (c *cmconn) Read(b []byte) (n int, err error) {
	return c.stream.Read(b)
}

func (c *cmconn) Write(b []byte) (n int, err error) {
	return c.stream.Write(b)
}

func (c *cmconn) Close() error {
	if !atomic.CompareAndSwapInt32(&c.closed, 0, 1) {
		return nil
	}
	err := c.stream.Close()
	c.onClose()
	return err
}

func (c *cmconn) LocalAddr() net.Addr {
	return c.wrapped.LocalAddr()
}

func (c *cmconn) RemoteAddr() net.Addr {
	return c.wrapped.RemoteAddr()
}

func (c *cmconn) SetDeadline(t time.Time) error {
	err := c.SetReadDeadline(t)
	if err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}

func (c *cmconn) SetReadDeadline(t time.Time) error {
	c.stream.SetReadDeadline(t)
	return nil
}

func (c *cmconn) SetWriteDeadline(t time.Time) error {
	// do nothing
	return nil
}
