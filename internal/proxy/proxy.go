// Package proxy implements the local HTTP forward proxy with optional HTTPS MITM.
package proxy

import (
	"bufio"
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/asmseo/mdcix/internal/convert"
	"github.com/asmseo/mdcix/internal/mitm"
	"github.com/asmseo/mdcix/internal/stats"
	"github.com/asmseo/mdcix/internal/tokens"
)

// Options configure the proxy server.
type Options struct {
	Addr      string
	Mode      convert.Mode
	MaxBody   int64
	Insecure  bool
	UserAgent string
	Counter   *tokens.Counter
	Session   *stats.Session
	MITM      *mitm.Manager // nil = CONNECT tunnel only (no conversion on HTTPS)
	Timeout   time.Duration
}

// Server is an HTTP forward proxy.
type Server struct {
	httpServer *http.Server
	transport  *MarkdownTransport
	opts       Options
}

// New builds a proxy Server.
func New(opts Options) *Server {
	if opts.Addr == "" {
		opts.Addr = ":8080"
	}
	if opts.Mode == "" {
		opts.Mode = convert.ModeReader
	}
	if opts.MaxBody <= 0 {
		opts.MaxBody = 8 << 20
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}

	inner := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: opts.Insecure, //nolint:gosec // intentional flag
		},
		IdleConnTimeout: 90 * time.Second,
	}

	mt := &MarkdownTransport{
		Inner:     inner,
		Mode:      opts.Mode,
		MaxBody:   opts.MaxBody,
		Counter:   opts.Counter,
		Session:   opts.Session,
		UserAgent: opts.UserAgent,
	}

	s := &Server{transport: mt, opts: opts}
	// No Read/WriteTimeout: CONNECT tunnels and SSE/streaming pass-through must not be cut mid-stream.
	// Header read is still bounded.
	s.httpServer = &http.Server{
		Addr:              opts.Addr,
		Handler:           http.HandlerFunc(s.handle),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

// ListenAndServe starts the proxy.
func (s *Server) ListenAndServe() error {
	log.Printf("mdcix proxy listening on %s (mode=%s, mitm=%v)", s.opts.Addr, s.opts.Mode, s.opts.MITM != nil)
	return s.httpServer.ListenAndServe()
}

// Close shuts down the listener.
func (s *Server) Close() error {
	return s.httpServer.Close()
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		if s.opts.MITM != nil {
			s.handleConnectMITM(w, r)
			return
		}
		s.handleConnectTunnel(w, r)
		return
	}
	s.handleHTTP(w, r)
}

func (s *Server) handleHTTP(w http.ResponseWriter, req *http.Request) {
	if !req.URL.IsAbs() {
		http.Error(w, "absolute URL required (use as HTTP proxy)", http.StatusBadRequest)
		return
	}
	req.RequestURI = ""
	removeHopByHop(req.Header)

	resp, err := s.transport.RoundTrip(req)
	if err != nil {
		http.Error(w, "upstream: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (s *Server) handleConnectTunnel(w http.ResponseWriter, req *http.Request) {
	dest, err := net.DialTimeout("tcp", req.Host, 10*time.Second)
	if err != nil {
		http.Error(w, "connect failed", http.StatusServiceUnavailable)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		dest.Close()
		http.Error(w, "hijack unsupported", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	client, _, err := hj.Hijack()
	if err != nil {
		dest.Close()
		return
	}
	go pipe(dest, client)
	go pipe(client, dest)
}

func (s *Server) handleConnectMITM(w http.ResponseWriter, req *http.Request) {
	cert, err := s.opts.MITM.GetCert(req.Host)
	if err != nil {
		http.Error(w, "cert: "+err.Error(), http.StatusInternalServerError)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack unsupported", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	client, _, err := hj.Hijack()
	if err != nil {
		return
	}
	defer client.Close()

	tlsConn := tls.Server(client, &tls.Config{
		Certificates: []tls.Certificate{*cert},
		MinVersion:   tls.VersionTLS12,
	})
	defer tlsConn.Close()
	if err := tlsConn.Handshake(); err != nil {
		return
	}

	reader := bufio.NewReader(tlsConn)
	for {
		creq, err := http.ReadRequest(reader)
		if err != nil {
			return
		}
		creq.RequestURI = ""
		creq.URL.Scheme = "https"
		creq.URL.Host = req.Host
		removeHopByHop(creq.Header)

		resp, err := s.transport.RoundTrip(creq)
		if err != nil {
			return
		}
		_ = resp.Write(tlsConn)
		resp.Body.Close()
	}
}

func pipe(dst io.WriteCloser, src io.ReadCloser) {
	defer dst.Close()
	defer src.Close()
	_, _ = io.Copy(dst, src)
}

func removeHopByHop(h http.Header) {
	for _, k := range []string{
		"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
		"TE", "Trailer", "Transfer-Encoding", "Upgrade", "Proxy-Connection",
	} {
		h.Del(k)
	}
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}
