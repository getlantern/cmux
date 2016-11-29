package cmux

import (
	"errors"
	"github.com/xtaci/smux"
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
	bufferSize            int
	nextConn              chan net.Conn
	nextErr               chan error
	sessions              map[int]*smux.Session
	nextSessionID         int
	closed                bool
	numConnections        int64
	numVirtualConnections int64
	mx                    sync.Mutex
}

// Listen creates a net.Listener that multiplexes connections over a connection
// obtained from the underlying opts.Listener.
func Listen(opts *ListenOpts) net.Listener {
	if opts.BufferSize <= 0 {
		opts.BufferSize = defaultBufferSize
	}
	l := &listener{
		wrapped:    opts.Listener,
		bufferSize: opts.BufferSize,
		nextConn:   make(chan net.Conn, 1),
		nextErr:    make(chan error, 1),
		sessions:   make(map[int]*smux.Session),
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
	smuxConfig := smux.DefaultConfig()
	smuxConfig.MaxReceiveBuffer = l.bufferSize
	session, err := smux.Server(conn, smuxConfig)
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
		l.mx.Lock()
		if !l.closed {
			session.Close()
			delete(l.sessions, sessionID)
			atomic.AddInt64(&l.numConnections, -1)
		}
		l.mx.Unlock()
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
	if l.closed {
		l.mx.Unlock()
		return nil
	}
	for _, session := range l.sessions {
		session.Close()
	}
	l.sessions = nil
	err := l.wrapped.Close()
	l.closed = true
	// Drain nextConn and nextErr
drain:
	for {
		select {
		case <-l.nextConn:
		case <-l.nextErr:
		default:
			break drain
		}
	}
	close(l.nextConn)
	l.mx.Unlock()
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
		time.Sleep(15 * time.Second)
		log.Debugf("Connections: %d   Virtual: %d", atomic.LoadInt64(&l.numConnections), atomic.LoadInt64(&l.numVirtualConnections))
		l.mx.Lock()
		closed := l.closed
		l.mx.Unlock()
		if closed {
			return
		}
	}
}
