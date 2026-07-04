package configutil

import (
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
)

func DecodeTOMLFileStrict(path string, v any) error {
	metadata, err := toml.DecodeFile(path, v)
	if err != nil {
		return fmt.Errorf("decode config %s: %w", path, err)
	}
	undecoded := metadata.Undecoded()
	if len(undecoded) == 0 {
		return nil
	}
	fields := make([]string, 0, len(undecoded))
	for _, key := range undecoded {
		fields = append(fields, key.String())
	}
	return fmt.Errorf("decode config %s: unknown fields: %s", path, strings.Join(fields, ", "))
}
