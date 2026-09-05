package session

import (
	"context"
	"fmt"

	provider "github.com/zhengjiarui/gaia-ai-provider"
	"github.com/zhengjiarui/gaia-harness/agent"
)

type AgentFactory func(Record) (*agent.Agent, error)
type Runner struct {
	Store    Store
	Service  Service
	NewAgent AgentFactory
}

func (r Runner) Run(ctx context.Context, id string, user provider.Message) (*provider.Response, error) {
	return r.RunWithEvents(ctx, id, user, nil)
}

func (r Runner) RunWithEvents(ctx context.Context, id string, user provider.Message, observer agent.EventObserver) (*provider.Response, error) {
	if r.Service.Store == nil || r.NewAgent == nil {
		return nil, fmt.Errorf("runner requires service store and agent factory")
	}
	if err := r.Service.Append(ctx, id, user); err != nil {
		return nil, err
	}
	record, err := r.Service.Store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	a, err := r.NewAgent(record)
	if err != nil {
		return nil, err
	}
	run, err := a.RunWithEvents(ctx, record.Messages, observer)
	if err != nil {
		return nil, err
	}
	response := run.Response
	if response == nil {
		return nil, fmt.Errorf("agent returned nil response")
	}
	if err = r.Service.AppendMessages(ctx, id, run.Messages); err != nil {
		return nil, err
	}
	return response, nil
}
