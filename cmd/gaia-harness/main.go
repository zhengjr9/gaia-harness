package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	provider "github.com/zhengjiarui/gaia-ai-provider"
	"github.com/zhengjiarui/gaia-harness/agent"
	"github.com/zhengjiarui/gaia-harness/httpapi"
	"github.com/zhengjiarui/gaia-harness/mcp"
	"github.com/zhengjiarui/gaia-harness/protocol"
	"github.com/zhengjiarui/gaia-harness/sandbox"
	"github.com/zhengjiarui/gaia-harness/session"
	"github.com/zhengjiarui/gaia-harness/skills"
	"github.com/zhengjiarui/gaia-harness/wasmext"
	_ "modernc.org/sqlite"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dbPath := flag.String("db", "gaia-harness.db", "sqlite database path")
	flag.Parse()
	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	store, err := session.NewSQLiteStore(db)
	if err != nil {
		log.Fatal(err)
	}
	workspaceRoot := os.Getenv("GAIA_WORKSPACE_ROOT")
	if workspaceRoot == "" {
		workspaceRoot, _ = os.Getwd()
	}
	_ = os.MkdirAll(workspaceRoot, 0700)
	modelProvider := os.Getenv("GAIA_PROVIDER")
	if modelProvider == "" {
		modelProvider = "ark"
	}
	modelID := os.Getenv("GAIA_MODEL")
	if modelID == "" {
		modelID = "deepseek-v4-flash"
	}
	baseURL := os.Getenv("GAIA_BASE_URL")
	if baseURL == "" {
		baseURL = "https://ark.cn-beijing.volces.com/api/plan/v3"
	}
	providers := provider.BuiltinProviders()
	knownProvider := false
	for _, spec := range provider.BuiltinSpecs() {
		if spec.ID == modelProvider {
			knownProvider = true
			break
		}
	}
	if !knownProvider {
		providers = append(providers, provider.NewOpenAI(provider.OpenAIConfig{ID: modelProvider, Name: modelProvider, BaseURL: baseURL, APIKey: os.Getenv("GAIA_API_KEY"), Models: []provider.Model{{ID: modelID, Name: modelID, Provider: modelProvider, ContextWindow: 128000, MaxTokens: 8192}}}))
	}
	registry := provider.NewRegistry(providers...)
	var sharedMCP mcp.Client
	var sharedMCPTools []agent.Tool
	if command := strings.TrimSpace(os.Getenv("GAIA_MCP_COMMAND")); command != "" {
		parts := strings.Fields(command)
		sharedMCP, err = mcp.NewStdio(context.Background(), parts[0], parts[1:]...)
		if err != nil {
			log.Fatal(err)
		}
		defer sharedMCP.Close()
		sharedMCPTools, err = mcp.AgentTools(context.Background(), sharedMCP)
		if err != nil {
			log.Fatal(err)
		}
	}
	var wasmMiddleware agent.Middleware
	var wasmHook *wasmext.WasmHook
	if path := strings.TrimSpace(os.Getenv("GAIA_WASM_HOOK")); path != "" {
		wasmHook, err = wasmext.Load(context.Background(), path)
		if err != nil {
			log.Fatal(err)
		}
		defer wasmHook.Close(context.Background())
		wasmMiddleware = wasmext.AgentMiddleware{Hook: wasmHook}
	}
	service := session.Service{Store: store, Compressor: session.TokenCompressor{ReserveOutput: 8192, SummaryPrefix: "Earlier context was compacted; rely on the retained transcript."}}
	runner := &session.Runner{Store: store, Service: service, NewAgent: func(record session.Record) (*agent.Agent, error) {
		workspace := record.CWD
		if workspace == "" {
			workspace = filepath.Join(workspaceRoot, record.WorkspaceID)
		}
		var sb sandbox.Sandbox
		var err error
		if os.Getenv("GAIA_SANDBOX_MODE") == "local" {
			sb, err = sandbox.NewLocal(workspace)
		} else {
			sb, err = sandbox.NewBwrap(sandbox.Config{Workspace: workspace, Network: true})
		}
		if err != nil {
			return nil, err
		}
		loader := skills.Filesystem{Root: filepath.Join(workspace, "skills")}
		tools := append(sandbox.Tools(sb), skills.Tools(loader)...)
		tools = append(tools, sharedMCPTools...)
		middleware := []agent.Middleware{}
		if wasmMiddleware != nil {
			middleware = append(middleware, wasmMiddleware)
		}
		return agent.New(agent.Config{Registry: registry, Model: record.Model, System: record.System, Tools: tools, Middleware: middleware})
	}}
	piServer := &protocol.Server{Sessions: service, Runner: runner, Registry: registry, CWD: workspaceRoot}
	mux := http.NewServeMux()
	mux.Handle("/v1/pi", piServer.NativeHandler())
	mux.Handle("/v1/sessions", (httpapi.Server{Sessions: service, Runner: runner}).Handler())
	mux.Handle("/v1/sessions/", (httpapi.Server{Sessions: service, Runner: runner}).Handler())
	log.Printf("gaia-harness listening on %s (cwd workspace: %s)", *addr, workspaceRoot)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
