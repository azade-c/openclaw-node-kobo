package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

type DialContextFunc func(ctx context.Context, network, addr string) (net.Conn, error)

type InvokeHandler func(ctx context.Context, req InvokeRequestParams) (interface{}, error)

type Client struct {
	url        string
	header     http.Header
	dialer     DialContextFunc
	logger     zerolog.Logger
	register   NodeRegistration
	onInvoke   InvokeHandler
	connMu     sync.Mutex
	conn       *websocket.Conn
	requestSeq atomic.Uint64
}

type Config struct {
	URL      string
	Header   http.Header
	Dialer   DialContextFunc
	Logger   zerolog.Logger
	Register NodeRegistration
	OnInvoke InvokeHandler
}

func New(cfg Config) *Client {
	return &Client{
		url:      cfg.URL,
		header:   cfg.Header,
		dialer:   cfg.Dialer,
		logger:   cfg.Logger,
		register: cfg.Register,
		onInvoke: cfg.OnInvoke,
	}
}

func (c *Client) Run(ctx context.Context) error {
	if c.dialer == nil {
		return errors.New("gateway: dialer required")
	}
	if c.onInvoke == nil {
		return errors.New("gateway: invoke handler required")
	}
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		conn, err := c.connect(ctx)
		if err != nil {
			c.logger.Warn().Err(err).Msg("gateway connect failed")
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		c.setConn(conn)
		if err := c.registerNode(ctx); err != nil {
			c.logger.Error().Err(err).Msg("gateway registration failed")
			c.closeConn()
			continue
		}
		if err := c.readLoop(ctx); err != nil {
			c.logger.Warn().Err(err).Msg("gateway read loop ended")
			c.closeConn()
			continue
		}
	}
}

func (c *Client) SendEvent(ctx context.Context, method string, params interface{}) error {
	env := Envelope{
		Method: method,
	}
	payload, err := json.Marshal(params)
	if err != nil {
		return err
	}
	env.Params = payload
	return c.send(ctx, env)
}

func (c *Client) send(ctx context.Context, env Envelope) error {
	conn := c.getConn()
	if conn == nil {
		return errors.New("gateway: no connection")
	}
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return conn.WriteMessage(websocket.TextMessage, data)
}

func (c *Client) connect(ctx context.Context) (*websocket.Conn, error) {
	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 10 * time.Second,
		NetDialContext:   c.dialer,
	}
	conn, _, err := dialer.DialContext(ctx, c.url, c.header)
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(8 << 20)
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	return conn, nil
}

func (c *Client) registerNode(ctx context.Context) error {
	id := c.nextID()
	params, err := json.Marshal(c.register)
	if err != nil {
		return err
	}
	idRaw := json.RawMessage([]byte(fmt.Sprintf("%q", id)))
	env := Envelope{
		ID:     &idRaw,
		Method: "node.register",
		Params: params,
	}
	if err := c.send(ctx, env); err != nil {
		return err
	}
	return nil
}

func (c *Client) readLoop(ctx context.Context) error {
	conn := c.getConn()
	if conn == nil {
		return errors.New("gateway: no connection")
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var env Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			c.logger.Warn().Err(err).Msg("gateway: invalid message")
			continue
		}
		if env.Method == "node.invoke.request" {
			if err := c.handleInvoke(ctx, env); err != nil {
				c.logger.Warn().Err(err).Msg("gateway: invoke handler error")
			}
			continue
		}
	}
}

func (c *Client) handleInvoke(ctx context.Context, env Envelope) error {
	var params InvokeRequestParams
	if err := json.Unmarshal(env.Params, &params); err != nil {
		return err
	}
	result, err := c.onInvoke(ctx, params)
	if env.ID != nil {
		return c.respondRPC(ctx, env.ID, result, err)
	}
	return c.respondEvent(ctx, params.RequestID, result, err)
}

func (c *Client) respondRPC(ctx context.Context, id *json.RawMessage, result interface{}, err error) error {
	var env Envelope
	if err != nil {
		env.ID = id
		env.Error = &RPCError{Code: 1, Message: err.Error()}
		return c.send(ctx, env)
	}
	resultRaw, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return marshalErr
	}
	env.ID = id
	env.Result = resultRaw
	return c.send(ctx, env)
}

func (c *Client) respondEvent(ctx context.Context, requestID string, result interface{}, err error) error {
	params := InvokeResultParams{RequestID: requestID}
	if err != nil {
		params.Error = &RPCError{Code: 1, Message: err.Error()}
	} else {
		params.Result = result
	}
	payload, marshalErr := json.Marshal(params)
	if marshalErr != nil {
		return marshalErr
	}
	env := Envelope{Method: "node.invoke.result", Params: payload}
	return c.send(ctx, env)
}

func (c *Client) nextID() string {
	val := c.requestSeq.Add(1)
	seed := rand.Int63n(9999)
	return fmt.Sprintf("%d-%d", val, seed)
}

func (c *Client) getConn() *websocket.Conn {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	return c.conn
}

func (c *Client) setConn(conn *websocket.Conn) {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	c.conn = conn
}

func (c *Client) closeConn() {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}
