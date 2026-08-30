//go:build js && wasm

// Command wasm is the browser entry point: the corpus engine compiled to
// WebAssembly, bridged to JavaScript through one global object.
//
// The bridge is deliberately narrow. JavaScript hands Go a *store* — an object
// with size(name) and readAt(name, off, len), both returning Promises — and Go
// hands JavaScript four async functions on globalThis.jhtEngine:
//
//	open(store)  -> Promise<summaryJSON>  reads manifest + footer, a few KB
//	load()       -> Promise<statsJSON>    materializes every row, the big fetch
//	search(json) -> Promise<responseJSON> in-memory scan, milliseconds
//	detail(url)  -> Promise<responseJSON> exact URL scan over the same rows
//
// Everything that can be tested without a browser lives in web/engine; this
// file is only the syscall/js plumbing, and it is exercised end to end under
// Node by web/test/smoke.mjs, which drives exactly the calls the page makes.
//
// Payloads cross the boundary as JSON strings rather than field-by-field
// js.Value traffic: one copy per call instead of hundreds, and both sides get
// to use their native JSON tooling.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"syscall/js"
	"time"

	"github.com/job-hunter-toolkit/job-hunter-toolkit/web/engine"
)

func main() {
	bridge := &bridge{}

	js.Global().Set("jhtEngine", js.ValueOf(map[string]any{
		"open":   promiseFunc(bridge.open),
		"load":   promiseFunc(bridge.load),
		"search": promiseFunc(bridge.search),
		"detail": promiseFunc(bridge.detail),
		"cancel": promiseFunc(bridge.cancel),
	}))

	// Signal readiness only after the functions exist, so the page cannot race
	// instantiation. The channel is what the exported callbacks run against;
	// main must never return or every js.FuncOf dies with it.
	if ready := js.Global().Get("jhtEngineReady"); ready.Type() == js.TypeFunction {
		ready.Invoke()
	}

	select {}
}

type bridge struct {
	engine   *engine.Engine
	mu       sync.Mutex
	searches map[int]context.CancelFunc
}

// open connects the engine to the JS store and reads the generation's metadata.
func (b *bridge) open(args []js.Value) (any, error) {
	if len(args) < 1 || args[0].Type() != js.TypeObject {
		return nil, errors.New("open: expected a store object with size() and readAt()")
	}

	eng, err := engine.Open(context.Background(), &jsStore{v: args[0]}, time.Now())
	if err != nil {
		return nil, err
	}

	b.engine = eng

	return marshal(eng.Summary())
}

// load materializes the rows. It is the one expensive call, so it reports what
// it cost: the page shows the number instead of guessing.
func (b *bridge) load(args []js.Value) (any, error) {
	if b.engine == nil {
		return nil, errors.New("load: open() first")
	}

	start := time.Now()
	var progress js.Value
	if len(args) > 0 && args[0].Type() == js.TypeFunction {
		progress = args[0]
	}

	if err := b.engine.LoadWithProgress(func(update engine.LoadProgress) {
		if progress.Type() == js.TypeFunction {
			progress.Invoke(map[string]any{
				"phase":     update.Phase,
				"completed": update.Completed, "total": update.Total,
			})
		}
	}); err != nil {
		return nil, err
	}

	return marshal(map[string]any{
		"rows":       b.engine.Summary().Rows,
		"elapsed_ms": time.Since(start).Milliseconds(),
	})
}

// search evaluates one JSON request against the loaded rows.
func (b *bridge) search(args []js.Value) (any, error) {
	if b.engine == nil || !b.engine.Loaded() {
		return nil, errors.New("search: load() first")
	}

	if len(args) < 1 || args[0].Type() != js.TypeString {
		return nil, errors.New("search: expected a JSON request string")
	}

	ctx := context.Background()
	token := 0
	if len(args) > 1 && args[1].Type() == js.TypeNumber {
		token = args[1].Int()
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		b.mu.Lock()
		if b.searches == nil {
			b.searches = make(map[int]context.CancelFunc)
		}
		b.searches[token] = cancel
		b.mu.Unlock()
		defer func() {
			b.mu.Lock()
			delete(b.searches, token)
			b.mu.Unlock()
			cancel()
		}()
	}

	out, err := b.engine.SearchJSONYielding(ctx, []byte(args[0].String()), yieldToEvents)
	if err != nil {
		return nil, err
	}

	return string(out), nil
}

// detail resolves a posting by exact snapshot URL over the existing compact
// projection. It deliberately shares search cancellation tokens and allocates
// no URL index.
func (b *bridge) detail(args []js.Value) (any, error) {
	if b.engine == nil || !b.engine.Loaded() {
		return nil, errors.New("detail: load() first")
	}
	if len(args) < 1 || args[0].Type() != js.TypeString {
		return nil, errors.New("detail: expected a URL string")
	}

	ctx := context.Background()
	token := 0
	if len(args) > 1 && args[1].Type() == js.TypeNumber {
		token = args[1].Int()
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		b.mu.Lock()
		if b.searches == nil {
			b.searches = make(map[int]context.CancelFunc)
		}
		b.searches[token] = cancel
		b.mu.Unlock()
		defer func() {
			b.mu.Lock()
			delete(b.searches, token)
			b.mu.Unlock()
			cancel()
		}()
	}

	out, err := b.engine.DetailJSONYielding(ctx, args[0].String(), yieldToEvents)
	if err != nil {
		return nil, err
	}

	return string(out), nil
}

// yieldToEvents gives the worker one task turn to receive cancellation. A
// resolved Promise only runs microtasks and cannot deliver a message event, so
// this deliberately waits on a zero-delay timer.
func yieldToEvents() error {
	executor := js.FuncOf(func(_ js.Value, args []js.Value) any {
		resolve := args[0]
		var callback js.Func
		callback = js.FuncOf(func(_ js.Value, _ []js.Value) any {
			defer callback.Release()
			resolve.Invoke()
			return nil
		})
		js.Global().Call("setTimeout", callback, 0)
		return nil
	})
	promise := js.Global().Get("Promise").New(executor)
	executor.Release()

	_, err := await(promise)
	return err
}

func (b *bridge) cancel(args []js.Value) (any, error) {
	if len(args) < 1 || args[0].Type() != js.TypeNumber {
		return nil, errors.New("cancel: expected a numeric search token")
	}

	b.mu.Lock()
	cancel := b.searches[args[0].Int()]
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}

	return nil, nil
}

func marshal(v any) (any, error) {
	out, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	return string(out), nil
}

// promiseFunc wraps a Go call as a JS async function.
//
// The work runs in a goroutine because it blocks — every store read awaits a
// JS Promise — and blocking the JS event loop from inside a js.FuncOf callback
// deadlocks the runtime. The wrapper resolves or rejects exactly once, and a
// panic rejects rather than killing the whole wasm instance silently.
func promiseFunc(run func(args []js.Value) (any, error)) js.Func {
	return js.FuncOf(func(_ js.Value, args []js.Value) any {
		executor := js.FuncOf(func(_ js.Value, promiseArgs []js.Value) any {
			resolve, reject := promiseArgs[0], promiseArgs[1]

			go func() {
				defer func() {
					if r := recover(); r != nil {
						reject.Invoke(jsError(fmt.Sprintf("panic: %v", r)))
					}
				}()

				result, err := run(args)
				if err != nil {
					reject.Invoke(jsError(err.Error()))

					return
				}

				resolve.Invoke(result)
			}()

			return nil
		})
		defer executor.Release() // the executor runs synchronously inside New

		return js.Global().Get("Promise").New(executor)
	})
}

func jsError(message string) js.Value {
	return js.Global().Get("Error").New(message)
}

// jsStore adapts the JS store object to corpus.Store. The contract on the JS
// side is two async methods:
//
//	size(name)            -> Promise<number>
//	readAt(name, off, n)  -> Promise<Uint8Array of exactly n bytes>
//
// Offsets and lengths travel as float64 because that is what JS numbers are; a
// corpus object bigger than 2^53 bytes is not a thing this project will ship.
type jsStore struct {
	v js.Value
}

func (s *jsStore) Size(_ context.Context, name string) (int64, error) {
	v, err := await(s.v.Call("size", name))
	if err != nil {
		return 0, fmt.Errorf("store size %s: %w", name, err)
	}

	return int64(v.Float()), nil
}

func (s *jsStore) ReadAt(_ context.Context, name string, p []byte, off int64) (int, error) {
	v, err := await(s.v.Call("readAt", name, float64(off), len(p)))
	if err != nil {
		return 0, fmt.Errorf("store read %s [%d,+%d): %w", name, off, len(p), err)
	}

	n := js.CopyBytesToGo(p, v)
	if n < len(p) {
		// io.ReaderAt's contract: a short read must error, or the table reader
		// would decode a truncated column as data.
		return n, io.ErrUnexpectedEOF
	}

	return n, nil
}

// await blocks the calling goroutine on a JS Promise. It must never run on the
// event loop itself, which is why every caller is inside promiseFunc's
// goroutine.
func await(promise js.Value) (js.Value, error) {
	type outcome struct {
		value js.Value
		err   error
	}

	done := make(chan outcome, 1)

	then := js.FuncOf(func(_ js.Value, args []js.Value) any {
		v := js.Undefined()
		if len(args) > 0 {
			v = args[0]
		}

		done <- outcome{value: v}

		return nil
	})
	defer then.Release()

	catch := js.FuncOf(func(_ js.Value, args []js.Value) any {
		reason := "promise rejected"
		if len(args) > 0 {
			if msg := args[0].Get("message"); msg.Type() == js.TypeString {
				reason = msg.String()
			} else {
				reason = args[0].String()
			}
		}

		done <- outcome{err: errors.New(reason)}

		return nil
	})
	defer catch.Release()

	promise.Call("then", then, catch)

	result := <-done

	return result.value, result.err
}
