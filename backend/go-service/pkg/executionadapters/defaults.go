package executionadapters

import (
	"alphaflow/go-service/pkg/executionadapter"
	"alphaflow/go-service/pkg/executionadapter/binance"
	"alphaflow/go-service/pkg/executionadapter/bitget"
	"alphaflow/go-service/pkg/executionadapter/deepcoin"
	"alphaflow/go-service/pkg/executionadapter/gate"
	"alphaflow/go-service/pkg/executionadapter/hotcoin"
	"alphaflow/go-service/pkg/executionadapter/weex"
)

func NewDefaultRegistry() (*executionadapter.Registry, error) {
	registry := executionadapter.NewRegistry()
	for _, register := range []func(*executionadapter.Registry) error{binance.Register, bitget.Register, gate.Register, weex.Register, deepcoin.Register, hotcoin.Register} {
		if err := register(registry); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
