package httpapi

import (
	"encoding/json"
	"net/http"

	provider "github.com/zhengjiarui/gaia-ai-provider"
	"github.com/zhengjiarui/gaia-harness/session"
)

type Server struct{ Sessions session.Service }

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sessions", s.create)
	mux.HandleFunc("GET /v1/sessions/{id}", s.get)
	mux.HandleFunc("POST /v1/sessions/{id}/messages", s.append)
	return mux
}
func (s Server) create(w http.ResponseWriter, r *http.Request) {
	var v session.Record
	if json.NewDecoder(r.Body).Decode(&v) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	if err := s.Sessions.Create(r.Context(), v); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	write(w, v)
}
func (s Server) get(w http.ResponseWriter, r *http.Request) {
	v, err := s.Sessions.Store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	write(w, v)
}
func (s Server) append(w http.ResponseWriter, r *http.Request) {
	var message provider.Message
	if json.NewDecoder(r.Body).Decode(&message) != nil {
		http.Error(w, "invalid message", 400)
		return
	}
	if err := s.Sessions.Append(r.Context(), r.PathValue("id"), message); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	write(w, map[string]string{"status": "ok"})
}
func write(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
