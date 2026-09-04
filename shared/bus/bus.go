package bus

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
)

type Bus struct {
	nc *nats.Conn
}

func Connect(url string) (*Bus, error) {
	nc, err := nats.Connect(url,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.ReconnectJitter(500*time.Millisecond, 2*time.Second),
		nats.ReconnectBufSize(16*1024*1024),
		nats.PingInterval(20*time.Second),
		nats.MaxPingsOutstanding(3),
		nats.FlusherTimeout(10*time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			slog.Warn("nats disconnected", "err", err)
		}),
		nats.ReconnectHandler(func(c *nats.Conn) {
			slog.Info("nats reconnected", "url", c.ConnectedUrl())
		}),
		nats.ClosedHandler(func(_ *nats.Conn) {
			slog.Error("nats connection closed permanently")
		}),
	)
	if err != nil {
		return nil, err
	}
	return &Bus{nc: nc}, nil
}

func (b *Bus) Close() { b.nc.Close() }

func (b *Bus) Publish(subject string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return b.nc.Publish(subject, data)
}

func Subscribe[T any](b *Bus, subject string, handler func(T)) (*nats.Subscription, error) {
	return b.nc.Subscribe(subject, func(msg *nats.Msg) {
		var v T
		if json.Unmarshal(msg.Data, &v) != nil {
			return
		}
		handler(v)
	})
}

func (b *Bus) RequestJSON(subject string, payload any, timeout time.Duration) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	msg, err := b.nc.Request(subject, data, timeout)
	if err != nil {
		return nil, err
	}
	return msg.Data, nil
}

func Respond[Req any, Resp any](b *Bus, subject, queue string, handler func(Req) Resp) (*nats.Subscription, error) {
	return b.nc.QueueSubscribe(subject, queue, func(msg *nats.Msg) {
		var req Req
		if json.Unmarshal(msg.Data, &req) != nil {
			return
		}
		if data, err := json.Marshal(handler(req)); err == nil {
			_ = msg.Respond(data)
		}
	})
}
