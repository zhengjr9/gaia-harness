package wasmext

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/tetratelabs/wazero"
	provider "github.com/zhengjiarui/gaia-ai-provider"
	"github.com/zhengjiarui/gaia-harness/agent"
)

type Hook interface {
	Invoke(context.Context, []byte) ([]byte, error)
}
type Middleware interface{ Hook }
type Func func(context.Context, []byte) ([]byte, error)

func (f Func) Invoke(ctx context.Context, b []byte) ([]byte, error) { return f(ctx, b) }

// WasmHook uses a small, language-neutral ABI. The module exports memory,
// alloc(i32) -> i32 and hook(i32,i32) -> i64. The returned i64 packs the
// output pointer in the high 32 bits and output length in the low 32 bits.
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
	alloc := instance.ExportedFunction("alloc")
	memory := instance.Memory()
	if fn == nil || alloc == nil || memory == nil {
		return nil, fmt.Errorf("wasm module must export memory, alloc and hook")
	}
	allocated, err := alloc.Call(ctx, uint64(len(data)))
	if err != nil || len(allocated) == 0 {
		return nil, fmt.Errorf("wasm alloc: %w", err)
	}
	ptr := uint32(allocated[0])
	if !memory.Write(ptr, data) {
		return nil, fmt.Errorf("wasm input write out of bounds")
	}
	result, err := fn.Call(ctx, uint64(ptr), uint64(len(data)))
	if err != nil || len(result) == 0 {
		if err == nil {
			err = fmt.Errorf("hook returned no result")
		}
		return nil, err
	}
	resultPtr := uint32(result[0] >> 32)
	resultLen := uint32(result[0])
	output, ok := memory.Read(resultPtr, resultLen)
	if !ok {
		return nil, fmt.Errorf("wasm output read out of bounds")
	}
	return append([]byte(nil), output...), nil
}
func (w *WasmHook) Close(ctx context.Context) error { return w.runtime.Close(ctx) }

type AgentMiddleware struct{ Hook Hook }

func (m AgentMiddleware) Before(ctx context.Context, req *provider.Request) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	data, err = m.Hook.Invoke(ctx, data)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, req)
}
func (m AgentMiddleware) After(ctx context.Context, res *provider.Response) error {
	data, err := json.Marshal(res)
	if err != nil {
		return err
	}
	data, err = m.Hook.Invoke(ctx, data)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, res)
}

var _ agent.Middleware = AgentMiddleware{}
