package cmux

import (
	"io"
	"math/rand"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInvalidProtocol(t *testing.T) {
	t.Parallel()

	// Make sure the peer message is larger than what smux will attempt to read.
	peerMsg := make([]byte, 256)
	_, err := rand.Read(peerMsg)
	require.NoError(t, err)

	_l, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	l := Listen(&ListenOpts{Listener: _l})
	defer l.Close()

	conn, err := net.Dial("tcp", l.Addr().String())
	require.NoError(t, err)

	_, err = conn.Write(peerMsg)
	require.NoError(t, err)

	_, err = l.Accept()
	require.Error(t, err)
	eip, ok := err.(*ErrorInvalidProtocol)
	require.True(t, ok)
	require.True(t, eip.StopCloseTimer())

	b := make([]byte, len(peerMsg))
	n, err := io.ReadFull(eip.ConnReader, b)
	require.NoError(t, err)
	require.Equal(t, peerMsg, b[:n])
}
