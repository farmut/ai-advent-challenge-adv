package fake

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MITMProxy — HTTP(S)-прокси, который вместо выхода в интернет
// терминирует TLS собственным CA и отдаёт запросы фейковым серверам по Host.
// Нужен для ботов, у которых base URL зашит в коде: подменяется только
// окружение (HTTPS_PROXY + доверенный CA), бинарник остаётся нетронутым.
type MITMProxy struct {
	routes map[string]http.Handler // hostname -> handler
	log    *log.Logger
	deny   atomic.Bool // режим «заглушки»: всё отклоняется 403, только фиксируется

	caCert *x509.Certificate
	caKey  *ecdsa.PrivateKey
	caPEM  []byte

	mu       sync.Mutex
	certs    map[string]*tls.Certificate
	connects []string
	tlsErrs  []string
}

// NewMITMProxy генерирует одноразовый CA и создаёт прокси.
func NewMITMProxy(routes map[string]http.Handler, logger *log.Logger) (*MITMProxy, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate CA key: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "weatherbot harness throwaway CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create CA cert: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parse CA cert: %w", err)
	}
	return &MITMProxy{
		routes: routes,
		log:    logger,
		caCert: cert,
		caKey:  key,
		caPEM:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		certs:  map[string]*tls.Certificate{},
	}, nil
}

// CAPEM — сертификат CA для SSL_CERT_FILE.
func (p *MITMProxy) CAPEM() []byte { return p.caPEM }

// Connects — цели CONNECT, которые запрашивал клиент.
func (p *MITMProxy) Connects() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.connects...)
}

// TLSErrors — ошибки TLS-рукопожатия (клиент не доверяет CA и т.п.).
func (p *MITMProxy) TLSErrors() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.tlsErrs...)
}

// SetDeny включает режим, в котором прокси не пускает никуда, а только
// записывает цели. Так стенд не даёт боту уйти в настоящий интернет.
func (p *MITMProxy) SetDeny(v bool) { p.deny.Store(v) }

func (p *MITMProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p.deny.Load() {
		p.mu.Lock()
		p.connects = append(p.connects, r.Host)
		p.mu.Unlock()
		p.log.Printf("PROXY deny %s %s", r.Method, r.Host)
		http.Error(w, "harness: outbound traffic blocked", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodConnect {
		// Обычный HTTP-прокси запрос с абсолютным URI.
		p.route(r.URL.Hostname()).ServeHTTP(w, r)
		return
	}
	host := r.URL.Hostname()
	if host == "" {
		host, _, _ = net.SplitHostPort(r.Host)
	}
	p.mu.Lock()
	p.connects = append(p.connects, r.Host)
	p.mu.Unlock()
	p.log.Printf("PROXY CONNECT %s", r.Host)

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack unsupported", http.StatusInternalServerError)
		return
	}
	conn, buf, err := hj.Hijack()
	if err != nil {
		p.log.Printf("PROXY hijack: %v", err)
		return
	}
	_ = conn.SetDeadline(time.Time{})
	if _, err := conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		conn.Close()
		return
	}
	var raw net.Conn = conn
	if buf.Reader.Buffered() > 0 {
		raw = &bufferedConn{Conn: conn, r: buf.Reader}
	}
	tconn := tls.Server(raw, &tls.Config{
		NextProtos: []string{"http/1.1"},
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			name := hello.ServerName
			if name == "" {
				name = host
			}
			return p.leaf(name)
		},
	})
	if err := tconn.Handshake(); err != nil {
		p.mu.Lock()
		p.tlsErrs = append(p.tlsErrs, fmt.Sprintf("%s: %v", r.Host, err))
		p.mu.Unlock()
		p.log.Printf("PROXY TLS handshake with client for %s failed: %v", r.Host, err)
		tconn.Close()
		return
	}
	srv := &http.Server{Handler: p.route(host), ReadHeaderTimeout: 10 * time.Second, ErrorLog: p.log}
	_ = srv.Serve(newOneConnListener(tconn))
}

func (p *MITMProxy) route(host string) http.Handler {
	if h, ok := p.routes[strings.ToLower(host)]; ok {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.log.Printf("PROXY no route for host %q", host)
		http.Error(w, "harness: no route for "+host, http.StatusBadGateway)
	})
}

func (p *MITMProxy) leaf(name string) (*tls.Certificate, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.certs[name]; ok {
		return c, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, p.caCert, &key.PublicKey, p.caKey)
	if err != nil {
		return nil, err
	}
	c := &tls.Certificate{Certificate: [][]byte{der, p.caCert.Raw}, PrivateKey: key}
	p.certs[name] = c
	return c, nil
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) { return b.r.Read(p) }

// oneConnListener отдаёт одно соединение и блокирует Accept до его закрытия.
type oneConnListener struct {
	conn net.Conn
	once sync.Once
	done chan struct{}
}

func newOneConnListener(c net.Conn) *oneConnListener {
	l := &oneConnListener{done: make(chan struct{})}
	l.conn = &closeNotifyConn{Conn: c, done: l.done}
	return l
}

func (l *oneConnListener) Accept() (net.Conn, error) {
	var c net.Conn
	l.once.Do(func() { c = l.conn })
	if c != nil {
		return c, nil
	}
	<-l.done
	return nil, net.ErrClosed
}

func (l *oneConnListener) Close() error   { return nil }
func (l *oneConnListener) Addr() net.Addr { return l.conn.LocalAddr() }

type closeNotifyConn struct {
	net.Conn
	done chan struct{}
	once sync.Once
}

func (c *closeNotifyConn) Close() error {
	c.once.Do(func() { close(c.done) })
	return c.Conn.Close()
}
