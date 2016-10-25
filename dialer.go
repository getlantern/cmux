package cmux

import (
	"github.com/xtaci/smux"
	"net"
	"sync"
)

// DialFN is a function that dials like net.Dial.
type DialFN func(network, addr string) (net.Conn, error)

type DialerOpts struct {
	Dial       DialFN
	PoolSize   int
	BufferSize int
}

type connAndSession struct {
	conn    net.Conn
	session *smux.Session
}

type dialer struct {
	dial        DialFN
	bufferSize  int
	poolSize    int
	currentConn int
	pool        map[string][]*connAndSession
	mx          sync.Mutex
}

// Dialer creates a DialFN that returns connections that multiplex themselves
// over a single connection obtained from the underlying opts.Dial function.
// It will continue to use that single connection until and unless it encounters
// an error creating a new multiplexed stream, at which point it will dial
// again.
func Dialer(opts *DialerOpts) DialFN {
	if opts.PoolSize < 1 {
		opts.PoolSize = 1
	}
	if opts.BufferSize <= 0 {
		opts.BufferSize = defaultBufferSize
	}
	d := &dialer{
		dial:       opts.Dial,
		bufferSize: opts.BufferSize,
		poolSize:   opts.PoolSize,
		pool:       make(map[string][]*connAndSession)}
	return d.Dial
}

func (d *dialer) Dial(network, addr string) (net.Conn, error) {
	d.mx.Lock()
	defer d.mx.Unlock()
	idx := d.currentConn % d.poolSize
	d.currentConn++
	conns := d.pool[addr]
	var cs *connAndSession
	if len(conns) > idx {
		cs = conns[idx]
	} else {
		var err error
		cs, err = d.connect(network, addr)
		if err != nil {
			return nil, err
		}
		conns = append(conns, cs)
		d.pool[addr] = conns
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
		conns[idx] = cs
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
	return &connAndSession{conn, session}, nil
}
