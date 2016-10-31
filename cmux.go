// Package cmux provides multiplexing over net.Conns using smux and adhering
// to standard net package interfaces.
package cmux

import (
	"github.com/getlantern/golog"
	"github.com/xtaci/smux"
	"net"
	"sync"
	"time"
)

var (
	log               = golog.LoggerFor("smuxconn")
	defaultBufferSize = 4194304
)

func newConn(wrapped net.Conn, readDeadline *deadline, writeDeadline *deadline, stream *smux.Stream, onClose func()) *cmconn {
	if onClose == nil {
		onClose = noop
	}
	return &cmconn{
		wrapped:       wrapped,
		readDeadline:  readDeadline,
		writeDeadline: writeDeadline,
		stream:        stream,
		onClose:       onClose,
	}
}

func noop() {}

type cmconn struct {
	wrapped       net.Conn
	readDeadline  *deadline
	writeDeadline *deadline
	stream        *smux.Stream
	onClose       func()
	closed        bool
	mx            sync.Mutex
}

func (c *cmconn) Read(b []byte) (n int, err error) {
	return c.stream.Read(b)
}

func (c *cmconn) Write(b []byte) (n int, err error) {
	return c.stream.Write(b)
}

func (c *cmconn) Close() error {
	c.mx.Lock()
	defer c.mx.Unlock()
	if c.closed {
		return nil
	}
	err := c.stream.Close()
	c.onClose()
	c.closed = true
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
	return c.readDeadline.set(c, t)
}

func (c *cmconn) SetWriteDeadline(t time.Time) error {
	return c.writeDeadline.set(c, t)
}
