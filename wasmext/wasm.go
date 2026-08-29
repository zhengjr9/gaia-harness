package wasmext

import (
	"context"
	"fmt"
	"os"

	"github.com/tetratelabs/wazero"
)

type Hook interface {
	Invoke(context.Context, []byte) ([]byte, error)
}
type Middleware interface{ Hook }
type Func func(context.Context, []byte) ([]byte, error)

func (f Func) Invoke(ctx context.Context, b []byte) ([]byte, error) { return f(ctx, b) }

// WasmHook is a deliberately small ABI boundary. A module exports `hook` and
// can use the host-provided memory ABI in a future version without changing
// the agent/session interfaces.
type WasmHook struct {
	runtime wazero.Runtime
	module  wazero.CompiledModule
}

func Load(ctx context.Context, path string) (*WasmHook, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	rt := wazero.NewRuntime(ctx)
	mod, err := rt.CompileModule(ctx, data)
	if err != nil {
		_ = rt.Close(ctx)
		return nil, err
	}
	return &WasmHook{runtime: rt, module: mod}, nil
}
func (w *WasmHook) Invoke(ctx context.Context, data []byte) ([]byte, error) {
	instance, err := w.runtime.InstantiateModule(ctx, w.module, wazero.NewModuleConfig())
	if err != nil {
		return nil, err
	}
	defer instance.Close(ctx)
	fn := instance.ExportedFunction("hook")
	if fn == nil {
		return nil, fmt.Errorf("wasm module does not export hook")
	}
	if _, err = fn.Call(ctx); err != nil {
		return nil, err
	}
	return data, nil
}
func (w *WasmHook) Close(ctx context.Context) error { return w.runtime.Close(ctx) }
