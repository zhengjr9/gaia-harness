package wasmext

import (
	"context"
	"testing"
)

func TestFunc(t *testing.T) {
	got, err := Func(func(_ context.Context, b []byte) ([]byte, error) { return append(b, '!'), nil }).Invoke(context.Background(), []byte("ok"))
	if err != nil || string(got) != "ok!" {
		t.Fatalf("%s %v", got, err)
	}
}
