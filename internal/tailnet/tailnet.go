package tailnet

import (
	"context"
	"net"

	"tailscale.com/tsnet"
)

type Config struct {
	Hostname string
	StateDir string
	Logf     func(format string, args ...interface{})
}

type Server struct {
	srv *tsnet.Server
}

func New(cfg Config) *Server {
	return &Server{
		srv: &tsnet.Server{
			Hostname: cfg.Hostname,
			Dir:      cfg.StateDir,
			Logf:     cfg.Logf,
		},
	}
}

func (s *Server) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return s.srv.Dial(ctx, network, address)
}

func (s *Server) Up(ctx context.Context) error {
	_, err := s.srv.Up(ctx)
	return err
}

func (s *Server) Close() error {
	return s.srv.Close()
}
