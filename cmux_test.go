package cmux

import (
	"github.com/getlantern/fdcount"
	"github.com/stretchr/testify/assert"
	"io"
	"net"
	"sync"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	_, fdc, err := fdcount.Matching("TCP")
	if err != nil {
		t.Fatal(err)
	}

	_l, err := net.Listen("tcp", "localhost:0")
	if !assert.NoError(t, err) {
		return
	}

	l := Listen(&ListenOpts{Listener: _l})
	assert.NoError(t, fdc.AssertDelta(1), "Starting listener should add only 1 file descriptor")

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		for {
			conn, acceptErr := l.Accept()
			if acceptErr != nil {
				log.Error(acceptErr)
				return
			}
			defer conn.Close()
			// Start echoing
			go io.Copy(conn, conn)
		}
	}()

	defer func() {
		l.Close()
		// Make sure we can close twice without problem
		l.Close()
		wg.Wait()
		assert.NoError(t, fdc.AssertDelta(0), "After closing listener, there should be no lingering file descriptors")
	}()

	dial := Dialer(&DialerOpts{Dial: net.Dial})

	c1, err := dial("tcp", l.Addr().String())
	if !assert.NoError(t, err) {
		return
	}
	defer c1.Close()
	assert.NoError(t, fdc.AssertDelta(3), "Dialing first connection should have added 2 file descriptors (one for each end of connection)")

	c2, err := dial("tcp", l.Addr().String())
	if !assert.NoError(t, err) {
		return
	}
	defer c2.Close()
	assert.NoError(t, fdc.AssertDelta(3), "Dialing second connection should not have added any file descriptors")

	_, err = c1.Write([]byte("c1"))
	if !assert.NoError(t, err) {
		return
	}
	_, err = c2.Write([]byte("c2"))
	if !assert.NoError(t, err) {
		return
	}

	buf := make([]byte, 2)
	_, err = io.ReadFull(c2, buf)
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, "c2", string(buf))
	_, err = io.ReadFull(c1, buf)
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, "c1", string(buf))
}
