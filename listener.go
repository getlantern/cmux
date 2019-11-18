package cmux

import (
	"bytes"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	pkgerrors "github.com/pkg/errors"

	"github.com/xtaci/smux"
)

// When a caller invokes Accept and we receive an invalid protocol from a peer, we return an error
// and the connection to the caller. To avoid leaks, we close the connection after a period if the
// caller has not already done so.
const invalidProtocolCloseDelay = time.Minute

var (
	ErrClosed = errors.New("listener closed")
)

type ListenOpts struct {
	Listener          net.Listener
	BufferSize        int
	KeepAliveInterval time.Duration
}

type listener struct {
	ListenOpts
	nextConn              chan net.Conn
	nextErr               chan error
	sessions              map[int]*smux.Session
	nextSessionID         int
	closeOnce             sync.Once
	chClosed              chan struct{}
	numConnections        int64
	numVirtualConnections int64
	mx                    sync.Mutex
}

// Listen creates a net.Listener that multiplexes connections over a connection
// obtained from the underlying opts.Listener. The Accept method will return
// ErrorInvalidProtocol if a peer attempts to connect but does not use the
// proper protocol.
func Listen(opts *ListenOpts) net.Listener {
	if opts.BufferSize <= 0 {
		opts.BufferSize = defaultBufferSize
	}
	l := &listener{
		ListenOpts: *opts,
		nextConn:   make(chan net.Conn, 1000),
		nextErr:    make(chan error, 1),
		sessions:   make(map[int]*smux.Session),
		chClosed:   make(chan struct{}),
	}
	go l.listen()
	go l.logStats()
	return l
}

func (l *listener) listen() {
	defer l.Close()
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			l.nextErr <- err
			return
		}
		atomic.AddInt64(&l.numConnections, 1)
		go l.handleConn(conn)
	}
}

func (l *listener) handleConn(conn net.Conn) {
	smuxConfig := smux.DefaultConfig()
	smuxConfig.MaxReceiveBuffer = l.BufferSize
	if l.KeepAliveInterval > 0 {
		smuxConfig.KeepAliveInterval = l.KeepAliveInterval
	}
	teeConn := newTeeConn(conn)
	session, err := smux.Server(teeConn, smuxConfig)
	if err != nil {
		l.nextErr <- err
		return
	}
	l.mx.Lock()
	sessionID := l.nextSessionID
	l.nextSessionID++
	l.sessions[sessionID] = session
	l.mx.Unlock()

	// Note: we do not defer conn.Close because we may want to pass on an unclosed connection when
	// receiving an invalid protocol.
	defer func() {
		session.Close()
		l.mx.Lock()
		delete(l.sessions, sessionID)
		l.mx.Unlock()
		atomic.AddInt64(&l.numConnections, -1)
	}()

	firstAccept := true
	for {
		stream, err := session.AcceptStream()
		if err != nil && firstAccept {
			// An error accepting the first stream may indicate problems with the connection or a
			// peer using an invalid protocol. These should be bubbled up to the caller.
			if pkgerrors.Cause(err) == smux.ErrInvalidProtocol {
				teeConn.cancelClose()
				eip := ErrorInvalidProtocol{Cause: err, Conn: conn, ConnReader: teeConn.teeReader}
				eip.startCloseTimer(invalidProtocolCloseDelay)
				l.nextErr <- &eip
			}
			return
		}
		if err != nil {
			log.Debugf("Error creating multiplexed session, probably just means that the underlying connection was closed: %v", err)
			conn.Close()
			return
		}
		firstAccept = false
		teeConn.stopTee()
		atomic.AddInt64(&l.numVirtualConnections, 1)
		l.nextConn <- &cmconn{
			Conn:    stream,
			onClose: l.cmconnClosed,
		}
	}
}

func (l *listener) Accept() (net.Conn, error) {
	select {
	case <-l.chClosed:
		return nil, ErrClosed
	case conn := <-l.nextConn:
		return conn, nil
	case err := <-l.nextErr:
		return nil, err
	}
}

func (l *listener) Close() (err error) {
	l.closeOnce.Do(func() {
		close(l.chClosed)
		for _, session := range l.sessions {
			closeErr := session.Close()
			if closeErr != nil {
				log.Errorf("Error closing session: %v", closeErr)
			}
		}
		err = l.Listener.Close()
		// Drain nextConn and nextErr
		for {
			select {
			case conn := <-l.nextConn:
				conn.Close()
			case <-l.nextErr:
			default:
				return
			}
		}
	})
	return
}

func (l *listener) Addr() net.Addr {
	return l.Listener.Addr()
}

func (l *listener) cmconnClosed() {
	atomic.AddInt64(&l.numVirtualConnections, -1)
}

func (l *listener) logStats() {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			log.Debugf("Connections: %d   Virtual: %d", atomic.LoadInt64(&l.numConnections), atomic.LoadInt64(&l.numVirtualConnections))
		case <-l.chClosed:
			log.Debug("Done logging stats")
			return
		}
	}
}

// teeConn allows users to read from a connection, but retains the data read in teeBuf.
type teeConn struct {
	conn      net.Conn
	teeReader io.Reader
	read      atomic.Value // type: func([]byte) (int, error)
	close     func() error
}

func newTeeConn(conn net.Conn) *teeConn {
	var (
		buf        = new(bytes.Buffer)
		connReader = io.TeeReader(conn, buf)
		teeReader  = io.MultiReader(buf, conn)
		read       = atomic.Value{}
	)
	read.Store(connReader.Read)
	return &teeConn{conn, teeReader, read, conn.Close}
}

func (tc *teeConn) Read(b []byte) (n int, err error) {
	return tc.read.Load().(func([]byte) (int, error))(b)
}

// Stops the tee process. Read can still be used to read from the connection. The teeReader will be
// set to nil.
func (tc *teeConn) stopTee() {
	tc.read.Store(tc.conn.Read)
	tc.teeReader = nil
}

func (tc *teeConn) Write(b []byte) (n int, err error) { return tc.conn.Write(b) }
func (tc *teeConn) Close() error                      { return tc.close() }
func (tc *teeConn) cancelClose()                      { tc.close = func() error { return nil } }

// ErrorInvalidProtocol is generated when a peer attempts to connect, but does not use the proper
// protocol.
type ErrorInvalidProtocol struct {
	Cause error
	Conn  net.Conn

	// Some data will have been read off the connection already, but ConnReader will have all data
	// transmitted by the peer.
	ConnReader io.Reader

	closeTimer         *time.Timer
	abandonTimer       chan struct{}
	stopCloseTimerOnce sync.Once
}

func (eip *ErrorInvalidProtocol) Error() string {
	return eip.Cause.Error()
}

func (eip *ErrorInvalidProtocol) startCloseTimer(d time.Duration) {
	eip.closeTimer = time.NewTimer(d)
	eip.abandonTimer = make(chan struct{})
	go func() {
		select {
		case <-eip.closeTimer.C:
			eip.Conn.Close()
		case <-eip.abandonTimer:
		}
	}()
}

// StopCloseTimer stops the timer controlling when the connection is closed. To avoid leaks, the
// connection is closed some time after this error is created. However, you can control closing the
// connection yourself by calling this function. The return value will be true if the call stops the
// timer and false if the timer has already expired or been stopped.
func (eip *ErrorInvalidProtocol) StopCloseTimer() bool {
	stopped := false
	eip.stopCloseTimerOnce.Do(func() {
		close(eip.abandonTimer)
		stopped = eip.closeTimer.Stop()
	})
	return stopped
}
