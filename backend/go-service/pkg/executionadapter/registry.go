package executionadapter

import (
	"alphaflow/go-service/pkg/executionaccount"
	"fmt"
	"strings"
	"sync"
)

type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

func NewRegistry() *Registry { return &Registry{factories: map[string]Factory{}} }
func (r *Registry) Register(exchange string, factory Factory) error {
	exchange = strings.ToLower(strings.TrimSpace(exchange))
	if exchange == "" || factory == nil {
		return fmt.Errorf("exchange and factory are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.factories[exchange]; ok {
		return fmt.Errorf("exchange %q already registered", exchange)
	}
	r.factories[exchange] = factory
	return nil
}
func (r *Registry) Build(account executionaccount.Account, credential executionaccount.Credential) (Adapter, error) {
	if err := account.Validate(); err != nil {
		return nil, err
	}
	if err := credential.Validate(account.Exchange); err != nil {
		return nil, err
	}
	r.mu.RLock()
	factory := r.factories[strings.ToLower(account.Exchange)]
	r.mu.RUnlock()
	if factory == nil {
		return nil, fmt.Errorf("exchange %q is not registered", account.Exchange)
	}
	return factory(account, credential)
}
