package wasmext

import (
	"context"
	"encoding/json"
	"testing"

	provider "github.com/zhengjiarui/gaia-ai-provider"
)

func TestFunc(t *testing.T) {
	got, err := Func(func(_ context.Context, b []byte) ([]byte, error) { return append(b, '!'), nil }).Invoke(context.Background(), []byte("ok"))
	if err != nil || string(got) != "ok!" {
		t.Fatalf("%s %v", got, err)
	}
}

func TestAgentMiddlewareTransformsPayload(t *testing.T) {
	mw := AgentMiddleware{Hook: Func(func(_ context.Context, b []byte) ([]byte, error) {
		var req provider.Request
		if err := json.Unmarshal(b, &req); err != nil {
			return nil, err
		}
		req.System = "changed"
		return json.Marshal(req)
	})}
	req := provider.Request{}
	if err := mw.Before(context.Background(), &req); err != nil {
		t.Fatal(err)
	}
	if req.System != "changed" {
		t.Fatal("wasm middleware did not transform request")
	}
}
