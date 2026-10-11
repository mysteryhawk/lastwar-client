package session

import (
	"context"
	"errors"
	"fmt"
	"lastwar-client/internal/sfs"
	"log/slog"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// EnvelopeDecodeError means framing succeeded and consumed a complete packet.
// It is safe to discard that packet; a framing/read error is never retried.
type EnvelopeDecodeError struct{ Err error }

func (e *EnvelopeDecodeError) Error() string { return "decode envelope: " + e.Err.Error() }
func (e *EnvelopeDecodeError) Unwrap() error { return e.Err }

var ErrReaderOwned = errors.New("socket reader belongs to dispatcher")

var ErrTooManyRequests = errors.New("too many pending requests")

const maxPendingRequests = 128

type pendingRequest struct {
	cmd    string
	result chan *ExtensionMessage
}

// Dispatcher exclusively owns socket reads after login/bootstrap. Never use
// ReadEnvelope, WaitFor or SendAndWait concurrently with it. Unmatched pushes
// are counted and discarded immediately; they cannot crowd out a response.
type Dispatcher struct {
	conn      *GameConn
	mu        sync.Mutex
	pending   map[int32]*pendingRequest
	err       error
	done      chan struct{}
	stopped   chan struct{}
	writes    chan struct{}
	exhausted bool
	unmatched atomic.Uint64
	malformed atomic.Uint64
}

// StartDispatcher is idempotent. Call only after synchronous bootstrap readers
// have returned. Request deadlines use contexts, never socket read deadlines.
func (c *GameConn) StartDispatcher() *Dispatcher {
	c.dispatchOnce.Do(func() {
		c.rmu.Lock()
		defer c.rmu.Unlock()
		d := &Dispatcher{conn: c, pending: make(map[int32]*pendingRequest), done: make(chan struct{}), stopped: make(chan struct{}), writes: make(chan struct{}, maxPendingRequests)}
		c.dispatcher = d
		if err := c.SetReadDeadline(time.Time{}); err != nil {
			d.fail(err)
			close(d.stopped)
			return
		}
		c.dispatchActive.Store(true)
		go d.readLoop()
	})
	return c.dispatcher
}

// Done closes on disconnection, terminal push, framing error or Close.
func (d *Dispatcher) Done() <-chan struct{} { return d.done }
func (d *Dispatcher) Err() error            { d.mu.Lock(); defer d.mu.Unlock(); return d.err }
func (d *Dispatcher) Unmatched() uint64     { return d.unmatched.Load() }
func (d *Dispatcher) Malformed() uint64     { return d.malformed.Load() }
func (d *Dispatcher) Close() error          { err := d.conn.Close(); <-d.stopped; return err }

func (d *Dispatcher) fail(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err == nil {
		d.err = err
		clear(d.pending)
		close(d.done)
	}
}

func (d *Dispatcher) readLoop() {
	defer close(d.stopped)
	for {
		env, err := d.conn.readEnvelopeLoop()
		if err != nil {
			var decode *EnvelopeDecodeError
			if errors.As(err, &decode) {
				d.malformed.Add(1)
				slog.Warn("discarding undecodable envelope")
				continue
			}
			var timeout net.Error
			if errors.As(err, &timeout) && timeout.Timeout() {
				err = sfs.NewDeadConnError(err)
			}
			d.fail(err)
			_ = d.conn.Close()
			return
		}
		msg, ok := env.AsExtension()
		if !ok {
			continue
		}
		v, ok := msg.Params.Get("_id")
		id, valid := v.Val.(int32)
		if !ok || !valid || id <= loginFutureID {
			d.unmatched.Add(1)
			continue
		}
		d.mu.Lock()
		p := d.pending[id]
		if p != nil && (msg.Cmd == p.cmd || msg.Cmd == "push."+p.cmd) {
			delete(d.pending, id)
			p.result <- msg // One slot per request; delivery never waits for the caller.
		} else {
			d.unmatched.Add(1)
		}
		d.mu.Unlock()
	}
}

// Request registers before write, rejects caller-supplied IDs, and accepts only
// the echoed Int ID and requested command (or push alias). Late/duplicate replies
// cannot satisfy a later request. Responses without IDs are never guessed.
func (d *Dispatcher) Request(ctx context.Context, cmd string, params *sfs.SFSObject) (*ExtensionMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cmd == "" {
		return nil, errors.New("empty command")
	}
	if params.Has("_id") {
		return nil, errors.New("dispatcher owns request _id")
	}
	d.mu.Lock()
	if d.err != nil {
		err := d.err
		d.mu.Unlock()
		return nil, err
	}
	if d.exhausted {
		d.mu.Unlock()
		return nil, errors.New("request ID sequence exhausted; reconnect")
	}
	if len(d.pending) >= maxPendingRequests {
		d.mu.Unlock()
		return nil, ErrTooManyRequests
	}
	select {
	case d.writes <- struct{}{}:
	default:
		d.mu.Unlock()
		return nil, ErrTooManyRequests
	}
	id := d.conn.extSent.Add(1) + loginFutureID
	if id <= loginFutureID || id == math.MaxInt32 {
		d.exhausted = true
		<-d.writes
		d.mu.Unlock()
		return nil, errors.New("request ID sequence exhausted; reconnect")
	}
	p := &pendingRequest{cmd: cmd, result: make(chan *ExtensionMessage, 1)}
	d.pending[id] = p
	d.mu.Unlock()
	defer func() { d.mu.Lock(); delete(d.pending, id); d.mu.Unlock() }()
	// The transport already bounds writes to ten seconds. Keep the request's
	// context responsive even if a peer stops reading; Close unblocks that write.
	sent := make(chan error, 1)
	// Copy the top-level parameters before returning on a cancelled context.
	frozen := sfs.NewSFSObject()
	if params != nil {
		for _, key := range params.Keys() {
			value, _ := params.Get(key)
			frozen.PutValue(key, value)
		}
	}
	go func() {
		defer func() { <-d.writes }()
		err := d.conn.sendExtensionID(cmd, frozen, id)
		if err != nil {
			d.fail(SendStageError{Err: err})
			_ = d.conn.Close()
		}
		sent <- err
	}()
	select {
	case err := <-sent:
		if err != nil {
			wrapped := SendStageError{Err: err}
			d.fail(wrapped)
			_ = d.conn.Close()
			return nil, wrapped
		}
	case <-d.done:
		return nil, d.Err()
	case <-ctx.Done():
		return nil, requestContextError(ctx)
	}
	select {
	case msg := <-p.result:
		return msg, nil
	case <-d.done:
		return nil, d.Err()
	case <-ctx.Done():
		return nil, requestContextError(ctx)
	}
}

func requestContextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &RequestTimeoutError{Err: ctx.Err()}
	}
	return ctx.Err()
}

type RequestTimeoutError struct{ Err error }

func (e *RequestTimeoutError) Error() string { return fmt.Sprintf("request timeout: %v", e.Err) }
func (e *RequestTimeoutError) Unwrap() error { return e.Err }
func (*RequestTimeoutError) Timeout() bool   { return true }
func (*RequestTimeoutError) Temporary() bool { return false }
