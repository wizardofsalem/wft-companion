package safe

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"runtime/debug"
	"sync"
	"time"
)

var (
	FirstPause = time.Second
	MaxPause   = 5 * time.Minute
	healthyRun = 10 * time.Minute
)

var (
	mu       sync.Mutex
	restarts = map[string]int{}
)

func Restarts() map[string]int {
	mu.Lock()
	defer mu.Unlock()
	out := make(map[string]int, len(restarts))
	maps.Copy(out, restarts)
	return out
}

func Call(name string, fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("recovered from a panic", "loop", name, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			err = fmt.Errorf("%s: panic: %v", name, r)
		}
	}()
	fn()
	return nil
}

func Go(ctx context.Context, name string, fn func(context.Context)) (done <-chan struct{}) {
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		pause := FirstPause
		for {
			started := time.Now()
			if Call(name, func() { fn(ctx) }) == nil {
				return
			}
			mu.Lock()
			restarts[name]++
			mu.Unlock()
			if time.Since(started) > healthyRun {
				pause = FirstPause
			}
			slog.Warn("restarting after a panic", "loop", name, "in", pause.String())
			select {
			case <-ctx.Done():
				return
			case <-time.After(pause):
			}
			pause *= 2
			if pause > MaxPause {
				pause = MaxPause
			}
		}
	}()
	return ch
}
