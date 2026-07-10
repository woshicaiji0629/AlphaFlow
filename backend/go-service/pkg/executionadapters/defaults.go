package executionadapters

import (
	"alphaflow/go-service/pkg/executionadapter"
	"alphaflow/go-service/pkg/executionadapter/binance"
	"alphaflow/go-service/pkg/executionadapter/bitget"
	"alphaflow/go-service/pkg/executionadapter/gate"
)

func NewDefaultRegistry() (*executionadapter.Registry, error) {
	registry := executionadapter.NewRegistry()
	for _, register := range []func(*executionadapter.Registry) error{binance.Register, bitget.Register, gate.Register} {
		if err := register(registry); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
