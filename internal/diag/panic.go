package diag

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"
)

// Restart is how long Supervise waits before it starts work again after a panic.
const Restart = time.Minute

// LogPanic writes a panic and its stack to the log at Error level. where names the work that panicked. It is meant to
// run while the panic is still unwinding, inside a deferred function, so that the stack shows where it came from.
func LogPanic(where string, value any) {
	slog.Error("Panic", "where", where, "panic", fmt.Sprint(value), "stack", string(debug.Stack()))
}

// Recover is deferred at the top of a goroutine: a panic is logged with its stack and the goroutine ends quietly.
func Recover(where string) {
	if value := recover(); value != nil {
		LogPanic(where, value)
	}
}

// Guard runs work, logs a panic in it with its stack, and reports whether there was one.
func Guard(where string, work func()) (panicked bool) {
	defer func() {
		if value := recover(); value != nil {
			LogPanic(where, value)
			panicked = true
		}
	}()
	work()
	return false
}

// Supervise runs work, the body of a long-lived loop, until it returns. After a panic it logs it, waits Restart so
// that bad data cannot make the loop spin, and runs work again, unless ctx ends first.
func Supervise(ctx context.Context, where string, work func()) {
	for Guard(where, work) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(Restart):
		}
	}
}
