package jsrt_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/jsrt"
)

func newTestEngine(t *testing.T) jsrt.Engine {
	t.Helper()
	eng, err := jsrt.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	return eng
}

func TestES2023AsyncAwaitAndExports(t *testing.T) {
	eng := newTestEngine(t)
	pool, err := jsrt.NewPool(eng, jsrt.PoolSpec{
		ID:       "async-test",
		Filename: "server.js",
		Code: []byte(`
export default {
  async greet(name) {
    const encoded = new TextEncoder().encode(name);
    const decoded = new TextDecoder().decode(encoded);
    await new Promise((resolve) => setTimeout(resolve, 15));
    const u = new URL('https://example.com/hello?a=1&b=two');
    return {
      greeting: "hello " + decoded,
      paramB: u.searchParams.get('b'),
      uuidLen: crypto.randomUUID().length,
      randBytesLen: crypto.getRandomValues(new Uint8Array(8)).length,
    };
  },
  async hostEcho(x) {
    return await hostMirror(x);
  },
  async failAsync() {
    await Promise.resolve();
    throw new Error("boom from async");
  },
};
`),
		Prepare: func(_ context.Context, inst jsrt.Instance) error {
			return inst.RegisterHostFunc("hostMirror", func(_ context.Context, args []any) (any, error) {
				if len(args) == 0 {
					return nil, nil
				}
				return map[string]any{"mirrored": args[0]}, nil
			})
		},
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.DrainAndClose(5 * time.Second)

	var res any
	_, err = pool.Do(context.Background(), func(inst jsrt.Instance) (jsrt.CallStats, error) {
		var s jsrt.CallStats
		var e error
		res, s, e = inst.InvokeExport(context.Background(), "greet", "世界")
		return s, e
	})
	if err != nil {
		t.Fatalf("greet: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", res)
	}
	if m["greeting"] != "hello 世界" || m["paramB"] != "two" ||
		m["uuidLen"] != float64(36) || m["randBytesLen"] != float64(8) {
		t.Fatalf("unexpected greet result: %#v", m)
	}

	// Host Promise bridge
	_, err = pool.Do(context.Background(), func(inst jsrt.Instance) (jsrt.CallStats, error) {
		var s jsrt.CallStats
		var e error
		res, s, e = inst.InvokeExport(context.Background(), "hostEcho", "ok")
		return s, e
	})
	if err != nil {
		t.Fatalf("hostEcho: %v", err)
	}
	if resMap, _ := res.(map[string]any); resMap["mirrored"] != "ok" {
		t.Fatalf("unexpected hostEcho result: %#v", res)
	}

	// Promise rejection becomes *jsrt.JSError
	_, err = pool.Do(context.Background(), func(inst jsrt.Instance) (jsrt.CallStats, error) {
		_, s, e := inst.InvokeExport(context.Background(), "failAsync")
		return s, e
	})
	if err == nil || !strings.Contains(err.Error(), "boom from async") {
		t.Fatalf("expected boom from async error, got %v", err)
	}
}

func TestInfiniteLoopPreemptionAndPoolRecovery(t *testing.T) {
	eng := newTestEngine(t)
	pool, err := jsrt.NewPool(eng, jsrt.PoolSpec{
		ID:       "loop-test",
		Filename: "server.js",
		Limits: jsrt.Limits{
			PoolSize: 2,
			Timeout:  2 * time.Second,
		},
		Code: []byte(`
export default {
  spin() {
    while (true) {}
  },
  ping() {
    return "pong";
  },
};
`),
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.DrainAndClose(5 * time.Second)

	start := time.Now()
	stats, err := pool.Do(context.Background(), func(inst jsrt.Instance) (jsrt.CallStats, error) {
		_, s, e := inst.InvokeExport(context.Background(), "spin")
		return s, e
	})
	elapsed := time.Since(start)
	if !errors.Is(err, jsrt.ErrTimeout) {
		t.Fatalf("expected ErrTimeout, got %v", err)
	}
	if !stats.TimedOut || !stats.Recreated {
		t.Fatalf("expected TimedOut & Recreated flags, got %#v", stats)
	}
	if elapsed > 2800*time.Millisecond {
		t.Fatalf("infinite loop preemption took too long: %s", elapsed)
	}

	// Subsequent calls across both pool slots must still succeed after replacement.
	for i := 0; i < 4; i++ {
		var out any
		_, err := pool.Do(context.Background(), func(inst jsrt.Instance) (jsrt.CallStats, error) {
			var s jsrt.CallStats
			var e error
			out, s, e = inst.InvokeExport(context.Background(), "ping")
			return s, e
		})
		if err != nil || out != "pong" {
			t.Fatalf("call %d after timeout failed: out=%v err=%v", i, out, err)
		}
	}

	deadline := time.Now().Add(3 * time.Second)
	for pool.Stats().Idle < 2 && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	st := pool.Stats()
	if st.Recreated != 1 || st.Timeouts != 1 || st.Idle != 2 {
		t.Fatalf("unexpected pool stats after recovery: %+v", st)
	}
}

func TestMemoryLimitOOMInterception(t *testing.T) {
	eng := newTestEngine(t)
	pool, err := jsrt.NewPool(eng, jsrt.PoolSpec{
		ID:       "oom-test",
		Filename: "server.js",
		Limits: jsrt.Limits{
			PoolSize:    1,
			MemoryLimit: jsrt.DefaultMemoryLimit, // 64 MiB
			Timeout:     5 * time.Second,
		},
		Code: []byte(`
export default {
  blowUp() {
    let s = "x".repeat(1024 * 1024);
    const keep = [s];
    while (true) {
      s = s + s;
      keep.push(s);
    }
  },
  ok() {
    return 42;
  },
};
`),
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.DrainAndClose(5 * time.Second)

	stats, err := pool.Do(context.Background(), func(inst jsrt.Instance) (jsrt.CallStats, error) {
		_, s, e := inst.InvokeExport(context.Background(), "blowUp")
		return s, e
	})
	if !errors.Is(err, jsrt.ErrOutOfMemory) {
		t.Fatalf("expected ErrOutOfMemory, got %v", err)
	}
	if !stats.OOM || !stats.Recreated {
		t.Fatalf("expected OOM and Recreated flags, got %#v", stats)
	}

	// Verify pool recovers with a fresh isolate after OOM.
	var res any
	_, err = pool.Do(context.Background(), func(inst jsrt.Instance) (jsrt.CallStats, error) {
		var s jsrt.CallStats
		var e error
		res, s, e = inst.InvokeExport(context.Background(), "ok")
		return s, e
	})
	if err != nil || res != float64(42) {
		t.Fatalf("post-OOM call failed: res=%v err=%v", res, err)
	}
}

func TestSandboxGlobalsAndConsoleBridge(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	eng := newTestEngine(t)
	pool, err := jsrt.NewPool(eng, jsrt.PoolSpec{
		ID:       "sandbox-test",
		Filename: "server.js",
		Log:      log,
		Code: []byte(`
export default {
  inspect() {
    console.info("hello log");
    return {
      hasOs: typeof globalThis.os !== "undefined",
      hasStd: typeof globalThis.std !== "undefined",
      hasProcess: typeof globalThis.process !== "undefined",
      hasSetInterval: typeof globalThis.setInterval !== "undefined",
    };
  },
};
`),
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.DrainAndClose(5 * time.Second)

	var out any
	_, err = pool.Do(context.Background(), func(inst jsrt.Instance) (jsrt.CallStats, error) {
		var s jsrt.CallStats
		var e error
		out, s, e = inst.InvokeExport(context.Background(), "inspect")
		return s, e
	})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	m := out.(map[string]any)
	for _, k := range []string{"hasOs", "hasStd", "hasProcess", "hasSetInterval"} {
		if m[k] != false {
			t.Fatalf("sandbox leaked %s = %v", k, m[k])
		}
	}
	if !strings.Contains(logBuf.String(), "hello log") || !strings.Contains(logBuf.String(), "plugin=sandbox-test") {
		t.Fatalf("console bridge missing expected log entry: %s", logBuf.String())
	}
}

func TestConcurrentPoolBorrowReturn(t *testing.T) {
	eng := newTestEngine(t)
	pool, err := jsrt.NewPool(eng, jsrt.PoolSpec{
		ID:       "conc-test",
		Filename: "server.js",
		Limits: jsrt.Limits{
			PoolSize: 2,
			Timeout:  2 * time.Second,
		},
		Code: []byte(`
export default {
  double(n) {
    return n * 2;
  },
};
`),
	})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.DrainAndClose(5 * time.Second)

	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 3; j++ {
				var out any
				_, err := pool.Do(context.Background(), func(inst jsrt.Instance) (jsrt.CallStats, error) {
					var s jsrt.CallStats
					var e error
					out, s, e = inst.InvokeExport(context.Background(), "double", i)
					return s, e
				})
				if err != nil {
					errs <- err
					return
				}
				if out != float64(i*2) {
					errs <- errors.New("wrong arithmetic result")
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent error: %v", err)
	}
}

func TestBundleSizeLimit(t *testing.T) {
	eng := newTestEngine(t)
	oversized := bytes.Repeat([]byte("a"), jsrt.MaxBundleBytes+1)
	_, err := jsrt.NewPool(eng, jsrt.PoolSpec{
		ID:       "big",
		Filename: "server.js",
		Code:     oversized,
	})
	if !errors.Is(err, jsrt.ErrBundleTooLarge) {
		t.Fatalf("expected ErrBundleTooLarge, got %v", err)
	}
}
