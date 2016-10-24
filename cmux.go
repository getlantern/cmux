// Package cmux provides multiplexing over net.Conns using smux and adhering
// to standard net package interfaces.
package cmux

import (
	"github.com/getlantern/golog"
	"github.com/xtaci/smux"
	"net"
	"time"
)

var (
	log = golog.LoggerFor("smuxconn")
)

type cmconn struct {
	wrapped net.Conn
	stream  *smux.Stream
}

func (c *cmconn) Read(b []byte) (n int, err error) {
	return c.stream.Read(b)
}

func (c *cmconn) Write(b []byte) (n int, err error) {
	return c.stream.Write(b)
}

func (c *cmconn) Close() error {
	return c.stream.Close()
}

func (c *cmconn) LocalAddr() net.Addr {
	return c.wrapped.LocalAddr()
}

func (c *cmconn) RemoteAddr() net.Addr {
	return c.wrapped.RemoteAddr()
}

func (c *cmconn) SetDeadline(t time.Time) error {
	// do nothing
	return nil
}

func (c *cmconn) SetReadDeadline(t time.Time) error {
	// do nothing
	return nil
}

func (c *cmconn) SetWriteDeadline(t time.Time) error {
	// do nothing
	return nil
}
