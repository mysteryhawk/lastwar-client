package app

import (
 "errors"
 "lastwar-client/internal/session"
 "log/slog"
 "net"
 "strings"
 "time"
)

// interactiveResult is delivered by the sole socket reader. Never read the
// underlying socket from the FIFO command goroutine while this reader runs.
type interactiveResult struct {
 msg *session.ExtensionMessage
 err error
}

// startInteractiveReader drains unsolicited pushes even when no FIFO command
// is being processed. A bounded channel protects against unbounded memory use.
func startInteractiveReader(conn *session.GameConn) <-chan interactiveResult {
 results := make(chan interactiveResult, 256)
 go func() {
  defer close(results)
  for {
   env, err := conn.ReadEnvelope()
   if err != nil {
    var netErr net.Error
    if errors.As(err, &netErr) && netErr.Timeout() {
     continue
    }
    select { case results <- interactiveResult{err:err}: default: }
    return
   }
   msg, ok := env.AsExtension()
   if !ok { continue }
   if strings.HasPrefix(msg.Cmd, "push.") {
    slog.Debug("unsolicited game push", "cmd", msg.Cmd)
   }
   select {
   case results <- interactiveResult{msg:msg}:
   default:
    // Never block the socket reader behind an idle command consumer.
    slog.Warn("interactive response queue full; dropping envelope", "cmd", msg.Cmd)
   }
  }
 }()
 return results
}

// waitInteractiveResponse consumes messages from the persistent reader rather
// than racing it for socket reads. Unrelated pushes are discarded here.
func waitInteractiveResponse(results <-chan interactiveResult, timeout time.Duration, commands ...string) (*session.ExtensionMessage, error) {
 timer := time.NewTimer(timeout)
 defer timer.Stop()
 for {
  select {
  case result, ok := <-results:
   if !ok { return nil, errors.New("interactive socket reader stopped") }
   if result.err != nil { return nil, result.err }
   for _, cmd := range commands {
    if result.msg.Cmd == cmd { return result.msg, nil }
   }
  case <-timer.C:
   return nil, session.DeadlineExceededError{}
  }
 }
}
