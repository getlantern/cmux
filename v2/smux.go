package cmux

import (
	"net"

	"github.com/xtaci/smux"
)

func translateSmuxErr(err error) error {
	if err == smux.ErrTimeout {
		return ErrTimeout
	} else {
		return err
	}
}

type smuxSession struct{ session *smux.Session }

func (s *smuxSession) AcceptStream() (net.Conn, error) { return s.session.AcceptStream() }
func (s *smuxSession) OpenStream() (net.Conn, error)   { return s.session.OpenStream() }
func (s *smuxSession) Close() error                    { return s.session.Close() }
func (s *smuxSession) NumStreams() int                 { return s.session.NumStreams() }

type smuxProtocol struct {
	smux.Config
}

func NewSmuxProtocol() *smuxProtocol {
	return &smuxProtocol{*smux.DefaultConfig()}
}

func (s *smuxProtocol) Client(conn net.Conn, opts *DialerOpts) (Session, error) {
	session, err := smux.Client(conn, &s.Config)
	return &smuxSession{session}, err
}

func (s *smuxProtocol) Server(conn net.Conn, opts *ListenOpts) (Session, error) {
	session, err := smux.Server(conn, &s.Config)
	return &smuxSession{session}, err
}

func (s *smuxProtocol) ErrorMapper() ErrorMapperFn {
	return translateSmuxErr
}
