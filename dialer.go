package cmux

import (
	"github.com/xtaci/smux"
	"net"
	"sync"
)

type DialFN func(network, addr string) (net.Conn, error)

type DialerOpts struct {
	Dial       DialFN
	BufferSize int
}

func Dialer(opts *DialerOpts) DialFN {
	if opts.BufferSize <= 0 {
		opts.BufferSize = 4194304
	}
	d := &dialer{dial: opts.Dial, bufferSize: opts.BufferSize, conns: make(map[string]*connAndSession)}
	return d.Dial
}

type connAndSession struct {
	conn    net.Conn
	session *smux.Session
}

type dialer struct {
	dial       DialFN
	bufferSize int
	conns      map[string]*connAndSession
	mx         sync.Mutex
}

func (d *dialer) Dial(network, addr string) (net.Conn, error) {
	d.mx.Lock()
	defer d.mx.Unlock()
	cs := d.conns[addr]
	if cs == nil {
		var err error
		cs, err = d.connect(network, addr)
		if err != nil {
			return nil, err
		}
	}
	stream, err := cs.session.OpenStream()
	if err != nil {
		// Reconnect and try again
		cs, err := d.connect(network, addr)
		if err != nil {
			return nil, err
		}
		stream, err = cs.session.OpenStream()
		if err != nil {
			return nil, err
		}
	}
	return &cmconn{cs.conn, stream}, nil
}

func (d *dialer) connect(network, addr string) (*connAndSession, error) {
	conn, err := d.dial(network, addr)
	if err != nil {
		return nil, err
	}
	smuxConfig := smux.DefaultConfig()
	smuxConfig.MaxReceiveBuffer = d.bufferSize
	session, err := smux.Client(conn, smuxConfig)
	if err != nil {
		return nil, err
	}
	cs := &connAndSession{conn, session}
	d.conns[addr] = cs
	return cs, nil
}
