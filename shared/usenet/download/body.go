package download

import (
	"context"
	"io"

	"github.com/Tensai75/nntpPool"
)

// readBody fetches and reads an article body, aborting the connection if ctx is
// cancelled. A client that seeks away cancels its request mid-fetch; without this
// the read would hold its pool connection until the whole article downloaded,
// starving the follow-up request (the stuck-on-seek bug in players like VLC).
func readBody(ctx context.Context, conn *nntpPool.NNTPConn, msgID string) ([]byte, error) {
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		body, err := conn.Body("<" + msgID + ">")
		if err != nil {
			ch <- result{nil, err}
			return
		}
		data, err := io.ReadAll(body)
		ch <- result{data, err}
	}()
	select {
	case <-ctx.Done():
		conn.Close()
		return nil, ctx.Err()
	case res := <-ch:
		return res.data, res.err
	}
}
