package session

import (
	"context"
	"errors"
	"lastwar-client/internal/sfs"
	"lastwar-client/internal/testutil"
	"testing"
	"time"
)

func requestTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestDispatcherDrainsIdleBurstAndCorrelates(t *testing.T) {
	c, s := NewPipeGameConnPair(t)
	d := c.StartDispatcher()
	if _, err := c.ReadEnvelope(); !errors.Is(err, ErrReaderOwned) {
		t.Fatal("raw reader not rejected")
	}
	if d != c.StartDispatcher() {
		t.Fatal("multiple dispatchers")
	}
	drained := make(chan struct{})
	go func() {
		for i := 0; i < 2001; i++ {
			if s.SendExtension("push.unrelated", nil) != nil {
				return
			}
		}
		close(drained)
		env, err := s.ReadEnvelope()
		if err != nil {
			return
		}
		msg, _ := env.AsExtension()
		p := sfs.NewSFSObject()
		p.PutInt("_id", msg.Params.GetInt("_id"))
		p.PutBool("success", true)
		_ = s.SendExtension("wrong.command", p)
		missing := sfs.NewSFSObject()
		missing.PutLong("_id", int64(msg.Params.GetInt("_id")))
		_ = s.SendExtension(msg.Cmd, missing)
		_ = s.SendExtension("push."+msg.Cmd, p)
	}()
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("idle backlog")
	}
	msg, err := d.Request(requestTestContext(t), "rank.test", nil)
	if err != nil || msg.Cmd != "push.rank.test" {
		t.Fatalf("response %v, %v", msg, err)
	}
	if d.Unmatched() != 2003 {
		t.Fatalf("unmatched %d", d.Unmatched())
	}
}

func TestDispatcherTimeoutStaleReplyAndCancellation(t *testing.T) {
	c, s := NewPipeGameConnPair(t)
	d := c.StartDispatcher()
	firstRead := make(chan int32, 1)
	release := make(chan struct{})
	go func() {
		env, err := s.ReadEnvelope()
		if err != nil {
			return
		}
		msg, _ := env.AsExtension()
		firstRead <- msg.Params.GetInt("_id")
		<-release
		env, err = s.ReadEnvelope()
		if err != nil {
			return
		}
		next, _ := env.AsExtension()
		stale := sfs.NewSFSObject()
		stale.PutInt("_id", msg.Params.GetInt("_id"))
		stale.PutLong("score", 99)
		_ = s.SendExtension(msg.Cmd, stale)
		fresh := sfs.NewSFSObject()
		fresh.PutInt("_id", next.Params.GetInt("_id"))
		fresh.PutLong("score", 123)
		_ = s.SendExtension(next.Cmd, fresh)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := d.Request(ctx, "rank.test", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout %v", err)
	}
	<-firstRead
	close(release)
	msg, err := d.Request(requestTestContext(t), "rank.test", nil)
	if err != nil || msg.Params.GetLong("score") != 123 {
		t.Fatalf("stale consumed: %v %v", msg, err)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err = d.Request(cancelled, "unused", nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	d.mu.Lock()
	n := len(d.pending)
	d.mu.Unlock()
	if n != 0 {
		t.Fatalf("pending leaked: %d", n)
	}
}

func TestDispatcherConcurrentOutOfOrder(t *testing.T) {
	c, s := NewPipeGameConnPair(t)
	d := c.StartDispatcher()
	go func() {
		msgs := make([]*ExtensionMessage, 0, 8)
		for i := 0; i < 8; i++ {
			env, err := s.ReadEnvelope()
			if err != nil {
				return
			}
			m, _ := env.AsExtension()
			msgs = append(msgs, m)
		}
		for i := len(msgs) - 1; i >= 0; i-- {
			p := sfs.NewSFSObject()
			p.PutInt("_id", msgs[i].Params.GetInt("_id"))
			p.PutLong("echo", msgs[i].Params.GetLong("index"))
			_ = s.SendExtension(msgs[i].Cmd, p)
		}
	}()
	results := make(chan error, 8)
	for i := int64(0); i < 8; i++ {
		go func(i int64) {
			p := sfs.NewSFSObject()
			p.PutLong("index", i)
			msg, err := d.Request(requestTestContext(t), "same.cmd", p)
			if err == nil && msg.Params.GetLong("echo") != i {
				err = errors.New("wrong response")
			}
			results <- err
		}(i)
	}
	for i := 0; i < 8; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}

func TestDispatcherDisconnectAndTerminal(t *testing.T) {
	for _, cmd := range []string{"disconnect", "push.user.off", "push.server.stop", "init.error"} {
		t.Run(cmd, func(t *testing.T) {
			c, s := NewPipeGameConnPair(t)
			d := c.StartDispatcher()
			go func() {
				_, err := s.ReadEnvelope()
				if err != nil {
					return
				}
				if cmd == "disconnect" {
					_ = s.Close()
				} else {
					_ = s.SendExtension(cmd, nil)
				}
			}()
			_, err := d.Request(requestTestContext(t), "rank.test", nil)
			if err == nil {
				t.Fatal("no failure")
			}
			if cmd != "disconnect" && !errors.Is(err, ErrSessionEnded) {
				t.Fatal(err)
			}
			if _, next := d.Request(requestTestContext(t), "rank.test", nil); next == nil {
				t.Fatal("reused dead connection")
			}
			if err := d.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDispatcherSkipsCompleteCorruptPacket(t *testing.T) {
	c, s := NewPipeGameConnPair(t)
	d := c.StartDispatcher()
	bad := testutil.MustEncodeCorruptPacket(t, "field", "value")
	go func() {
		env, err := s.ReadEnvelope()
		if err != nil {
			return
		}
		m, _ := env.AsExtension()
		_, _ = s.RawConn().Write(bad)
		p := sfs.NewSFSObject()
		p.PutInt("_id", m.Params.GetInt("_id"))
		_ = s.SendExtension(m.Cmd, p)
	}()
	if _, err := d.Request(requestTestContext(t), "rank.test", nil); err != nil {
		t.Fatal(err)
	}
	if d.Malformed() != 1 {
		t.Fatal("corrupt packet not counted")
	}
}

func TestDispatcherRejectsCallerIDAndClearsBootstrapDeadline(t *testing.T) {
	c, s := NewPipeGameConnPair(t)
	_ = c.SetReadDeadline(time.Now().Add(-time.Second))
	d := c.StartDispatcher()
	p := sfs.NewSFSObject()
	p.PutInt("_id", 50)
	if _, err := d.Request(requestTestContext(t), "rank", p); err == nil {
		t.Fatal("accepted caller id")
	}
	go func() {
		env, err := s.ReadEnvelope()
		if err != nil {
			return
		}
		m, _ := env.AsExtension()
		p := sfs.NewSFSObject()
		p.PutInt("_id", m.Params.GetInt("_id"))
		_ = s.SendExtension(m.Cmd, p)
	}()
	if _, err := d.Request(requestTestContext(t), "rank", nil); err != nil {
		t.Fatal(err)
	}
}
