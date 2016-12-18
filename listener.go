package cmux

import (
	"errors"
	"github.com/whyrusleeping/yamux"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrClosed = errors.New("listener closed")
)

type ListenOpts struct {
	Listener   net.Listener
	BufferSize int
}

type listener struct {
	wrapped               net.Listener
	nextConn              chan net.Conn
	nextErr               chan error
	sessions              map[int]*yamux.Session
	nextSessionID         int
	closed                bool
	numConnections        int64
	numVirtualConnections int64
	mx                    sync.Mutex
}

// Listen creates a net.Listener that multiplexes connections over a connection
// obtained from the underlying opts.Listener.
func Listen(opts *ListenOpts) net.Listener {
	l := &listener{
		wrapped:  opts.Listener,
		nextConn: make(chan net.Conn, 1),
		nextErr:  make(chan error, 1),
		sessions: make(map[int]*yamux.Session),
	}
	go l.listen()
	go l.logStats()
	return l
}

func (l *listener) listen() {
	defer l.Close()
	defer close(l.nextErr)
	for {
		conn, err := l.wrapped.Accept()
		if err != nil {
			l.nextErr <- err
			return
		}
		atomic.AddInt64(&l.numConnections, 1)
		go l.handleConn(conn)
	}
}

func (l *listener) handleConn(conn net.Conn) {
	l.mx.Lock()
	config := yamux.DefaultConfig()
	session, err := yamux.Server(conn, config)
	if err != nil {
		l.nextErr <- err
		l.mx.Unlock()
		return
	}
	sessionID := l.nextSessionID
	l.nextSessionID++
	l.sessions[sessionID] = session
	l.mx.Unlock()

	defer func() {
		session.Close()
		conn.Close()
		l.mx.Lock()
		delete(l.sessions, sessionID)
		l.mx.Unlock()
		atomic.AddInt64(&l.numConnections, -1)
	}()

	for {
		stream, err := session.AcceptStream()
		if err != nil {
			log.Debugf("Error creating multiplexed session, probably just means that the underlying connection was closed: %v", err)
			return
		}
		atomic.AddInt64(&l.numVirtualConnections, 1)
		l.nextConn <- &cmconn{
			wrapped: conn,
			stream:  stream,
			onClose: l.cmconnClosed,
		}
	}
}

func (l *listener) Accept() (net.Conn, error) {
	select {
	case conn, ok := <-l.nextConn:
		if !ok {
			return nil, ErrClosed
		}
		return conn, nil
	case err, ok := <-l.nextErr:
		if !ok {
			return nil, ErrClosed
		}
		return nil, err
	}
}

func (l *listener) Close() error {
	l.mx.Lock()
	defer l.mx.Unlock()
	if l.closed {
		return nil
	}
	for _, session := range l.sessions {
		session.Close()
	}
	l.closed = true
	err := l.wrapped.Close()
	// Drain nextConn and nextErr
drain:
	for {
		select {
		case conn := <-l.nextConn:
			conn.Close()
		case <-l.nextErr:
		default:
			break drain
		}
	}
	close(l.nextConn)
	return err
}

func (l *listener) Addr() net.Addr {
	return l.wrapped.Addr()
}

func (l *listener) cmconnClosed() {
	atomic.AddInt64(&l.numVirtualConnections, -1)
}

func (l *listener) logStats() {
	for {
		time.Sleep(5 * time.Second)
		log.Debugf("Connections: %d   Virtual: %d", atomic.LoadInt64(&l.numConnections), atomic.LoadInt64(&l.numVirtualConnections))
		l.mx.Lock()
		closed := l.closed
		l.mx.Unlock()
		if closed {
			log.Debug("Done logging stats")
			return
		}
	}
}
