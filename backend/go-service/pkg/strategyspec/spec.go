package strategyspec

import "strings"

// Spec describes one configured strategy definition and its parameters.
type Spec struct {
	Name    string            `toml:"name" json:"name"`
	Enabled bool              `toml:"enabled" json:"enabled"`
	Params  map[string]string `toml:"params" json:"params,omitempty"`
}

func Normalize(spec Spec) Spec {
	spec.Name = strings.ToLower(strings.TrimSpace(spec.Name))
	if spec.Params == nil {
		spec.Params = map[string]string{}
	}
	return spec
}

func Legacy(name string) Spec {
	spec := Normalize(Spec{Name: name, Enabled: true})
	return spec
}
