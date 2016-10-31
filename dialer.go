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
	dialer  *dialer
	addr    string
	idx     int
}

type dialer struct {
	dial          DialFN
	bufferSize    int
	poolSize      int
	currentConnID int
	pool          map[string]map[int]*connAndSession
	mx            sync.Mutex
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
		pool:       make(map[string]map[int]*connAndSession)}
	return d.Dial
}

func (d *dialer) Dial(network, addr string) (net.Conn, error) {
	d.mx.Lock()
	defer d.mx.Unlock()

	idx := d.currentConnID % d.poolSize
	d.currentConnID++

	var cs *connAndSession

	// Create pool if necessary
	conns := d.pool[addr]
	if conns == nil {
		conns = make(map[int]*connAndSession, d.poolSize)
		d.pool[addr] = conns
	} else {
		cs = conns[idx]
	}

	// Create conn if necessary
	if cs == nil {
		var err error
		cs, err = d.connect(network, addr, idx)
		if err != nil {
			return nil, err
		}
		conns[idx] = cs
	}

	// Open stream
	stream, err := cs.session.OpenStream()
	if err != nil {
		// Reconnect and try again
		cs, err := d.connect(network, addr, idx)
		if err != nil {
			return nil, err
		}
		stream, err = cs.session.OpenStream()
		if err != nil {
			return nil, err
		}
		conns[idx] = cs
	}

	return newConn(cs.conn, newDeadline(cs.conn.SetReadDeadline), newDeadline(cs.conn.SetWriteDeadline), stream, cs.closeIfNecessary), nil
}

func (d *dialer) connect(network, addr string, idx int) (*connAndSession, error) {
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
	return &connAndSession{
		conn:    conn,
		session: session,
		dialer:  d,
		addr:    addr,
		idx:     idx,
	}, nil
}

func (cs *connAndSession) closeIfNecessary() {
	if cs.session.NumStreams() == 0 {
		// Closing session also closes connection
		cs.session.Close()
		cs.dialer.removeFromPool(cs.addr, cs.idx)
	}
}

func (d *dialer) removeFromPool(addr string, idx int) {
	d.mx.Lock()
	defer d.mx.Unlock()
	conns := d.pool[addr]
	if conns != nil {
		delete(conns, idx)
		if len(conns) == 0 {
			delete(d.pool, addr)
		}
	}
}
