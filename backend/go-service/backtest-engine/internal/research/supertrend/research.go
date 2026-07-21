package supertrend

import (
	"context"
)

func Run(ctx context.Context, args []string) error {
	options, err := parseCommandOptions(args)
	if err != nil {
		return err
	}
	return run(ctx, options)
}
