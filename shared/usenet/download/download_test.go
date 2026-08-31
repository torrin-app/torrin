package download

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tensai75/nntpPool"
)

type mockNNTPServer struct {
	listener net.Listener
	done     chan struct{}
	mu       sync.Mutex
	commands []string
}

func newMockNNTPServer(t *testing.T) *mockNNTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &mockNNTPServer{listener: ln, done: make(chan struct{})}
	go s.serve()
	return s
}

func (s *mockNNTPServer) serve() {
	defer close(s.done)
	conn, err := s.listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	fmt.Fprint(conn, "200 mock nntp ready\r\n")
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		command := scanner.Text()
		s.mu.Lock()
		s.commands = append(s.commands, command)
		s.mu.Unlock()
		switch {
		case strings.HasPrefix(command, "AUTHINFO USER "):
			fmt.Fprint(conn, "381 password required\r\n")
		case strings.HasPrefix(command, "AUTHINFO PASS "):
			fmt.Fprint(conn, "281 authentication accepted\r\n")
		case command == "DATE":
			fmt.Fprint(conn, "111 20260830010101\r\n")
		case command == "GROUP alt.test":
			fmt.Fprint(conn, "211 1 1 1 alt.test\r\n")
		case command == "BODY <part-1>":
			fmt.Fprint(conn, "222 1 <part-1> body follows\r\n=ybegin line=128 size=4 name=x.bin\r\nklmn\r\n=yend size=4\r\n.\r\n")
		case command == "QUIT":
			fmt.Fprint(conn, "205 closing connection\r\n")
			return
		default:
			fmt.Fprint(conn, "500 unexpected command\r\n")
		}
	}
}

func (s *mockNNTPServer) close(t *testing.T) {
	t.Helper()
	s.listener.Close()
	select {
	case <-s.done:
	case <-time.After(time.Second):
		t.Fatal("mock NNTP server did not stop")
	}
}

func (s *mockNNTPServer) addr() (string, int) {
	host, port, _ := net.SplitHostPort(s.listener.Addr().String())
	var n int
	fmt.Sscanf(port, "%d", &n)
	return host, n
}

func (s *mockNNTPServer) commandLog() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.commands, "\n")
}

type blockingGetPool struct {
	entered chan struct{}
	release chan struct{}
	closed  bool
}

func (*blockingGetPool) Conns() (uint32, uint32) { return 0, 0 }
func (*blockingGetPool) MaxConns() uint32        { return 1 }
func (p *blockingGetPool) Get(ctx context.Context) (*nntpPool.NNTPConn, error) {
	if ctx.Done() != nil {
		return nil, errors.New("pool acquisition context must not be cancelable")
	}
	close(p.entered)
	<-p.release
	return nil, errors.New("pool unavailable")
}
func (*blockingGetPool) Put(*nntpPool.NNTPConn) {}
func (p *blockingGetPool) Close()               { p.closed = true }

func TestFetchSegmentFailover(t *testing.T) {
	orig := fetchOne
	defer func() { fetchOne = orig }()

	var calls int
	fetchOne = func(_ context.Context, _ nntpPool.ConnectionPool, _, _ string) ([]byte, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("430 no such article")
		}
		return []byte("payload"), nil
	}
	data, err := fetchSegment(context.Background(), make([]nntpPool.ConnectionPool, 2), "mid", "grp")
	if err != nil || string(data) != "payload" || calls != 2 {
		t.Fatalf("failover: data=%q err=%v calls=%d (want payload, nil, 2)", data, err, calls)
	}
}

func TestFetchSegmentAllMissing(t *testing.T) {
	orig := fetchOne
	defer func() { fetchOne = orig }()
	fetchOne = func(_ context.Context, _ nntpPool.ConnectionPool, _, _ string) ([]byte, error) {
		return nil, errors.New("430 no such article")
	}
	if _, err := fetchSegment(context.Background(), make([]nntpPool.ConnectionPool, 3), "mid", "grp"); err == nil {
		t.Fatal("all providers missing should return an error")
	}
}

func TestAcquireSharedRefCount(t *testing.T) {
	sharedMu.Lock()
	sharedPools = map[string]*sharedEntry{}
	sharedMu.Unlock()

	c := Credentials{Host: "test.invalid", Port: 119, Username: "u", MaxConns: 4}
	key := c.Host + "|" + c.Username

	refs := func() int {
		sharedMu.Lock()
		defer sharedMu.Unlock()
		if e, ok := sharedPools[key]; ok {
			return e.refs
		}
		return -1
	}

	p1, rel1, err := AcquireShared(c)
	if err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	p2, rel2, err := AcquireShared(c)
	if err != nil {
		t.Fatalf("acquire 2: %v", err)
	}
	if p1 != p2 {
		t.Fatal("concurrent acquirers must share one pool")
	}
	if got := refs(); got != 2 {
		t.Fatalf("refs = %d, want 2", got)
	}

	rel1()
	if got := refs(); got != 1 {
		t.Fatalf("after one release refs = %d, want 1 (pool must stay while held)", got)
	}

	rel2()
	if got := refs(); got != -1 {
		t.Fatalf("after last release pool must be removed, refs = %d", got)
	}

	rel2()
	if got := refs(); got != -1 {
		t.Fatal("double release must be a no-op")
	}

	p3, rel3, err := AcquireShared(c)
	if err != nil {
		t.Fatalf("re-acquire: %v", err)
	}
	if p3 == p1 {
		t.Fatal("re-acquire after full release must build a fresh pool, not reuse the closed one")
	}
	rel3()
}

func TestIsOptional(t *testing.T) {
	skip := []string{"cover.jpg", "poster.PNG", "info.nfo", "files.sfv", "readme.txt"}
	keep := []string{"movie.mkv", "movie.part01.rar", "data.r00", "archive.par2", "ep.s01e02.mp4"}
	for _, n := range skip {
		if !isOptional(n) {
			t.Errorf("%q should be optional (skipped)", n)
		}
	}
	for _, n := range keep {
		if isOptional(n) {
			t.Errorf("%q should NOT be optional (par2 kept for repair, media kept)", n)
		}
	}
}

func TestPoolArticleFetcherAgainstMockNNTP(t *testing.T) {
	server := newMockNNTPServer(t)
	host, port := server.addr()
	pool, err := NewPool(Credentials{
		Host: host, Port: port, Username: "alice", Password: "secret", MaxConns: 1,
	})
	if err != nil {
		server.close(t)
		t.Fatal(err)
	}
	body, err := NewArticleFetcher([]nntpPool.ConnectionPool{pool}).Fetch(context.Background(), "part-1", "alt.test")
	if err != nil {
		pool.Close()
		server.close(t)
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "=ybegin") || !strings.Contains(string(body), "klmn") {
		t.Fatalf("body = %q", body)
	}
	commands := server.commandLog()
	for _, want := range []string{
		"AUTHINFO USER alice", "AUTHINFO PASS secret", "DATE",
		"GROUP alt.test", "BODY <part-1>",
	} {
		if !strings.Contains(commands, want) {
			t.Fatalf("missing command %q in:\n%s", want, commands)
		}
	}
	pool.Close()
	server.close(t)
}

func TestCanceledFetchDoesNotCloseSharedPool(t *testing.T) {
	pool := &blockingGetPool{entered: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := fetchSegment(ctx, []nntpPool.ConnectionPool{pool}, "part-1", "alt.test")
		errCh <- err
	}()
	<-pool.entered
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("fetch error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled fetch kept waiting for the pool")
	}
	close(pool.release)
	if pool.closed {
		t.Fatal("one canceled fetch closed the shared pool")
	}
}

func TestIsArticleMissing(t *testing.T) {
	for _, err := range []error{
		errors.New("430 no such article"), errors.New("423 article not found"),
	} {
		if !isArticleMissing(err) {
			t.Fatalf("%q should be a missing article", err)
		}
	}
	if isArticleMissing(errors.New("temporary connection failure")) {
		t.Fatal("transient error reported as a missing article")
	}
}
