package experiments

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

var (
	ErrRegistryFinished = errors.New("experiment registry already finished")
	ErrNilExperiment    = errors.New("experiment is nil")
)

// Registry runs experiments in explicit registration order.
type Registry struct {
	experiments []Experiment
	finished    bool
}

// NewRegistry validates the experiment set and preserves its supplied order.
func NewRegistry(items ...Experiment) (*Registry, error) {
	experiments := append([]Experiment(nil), items...)
	seen := make(map[string]struct{}, len(experiments))
	for index, experiment := range experiments {
		if isNilExperiment(experiment) {
			return nil, fmt.Errorf("experiment %d: %w", index, ErrNilExperiment)
		}
		descriptor := experiment.Descriptor()
		if err := validateDescriptor(descriptor); err != nil {
			return nil, fmt.Errorf("experiment %d: %w", index, err)
		}
		if _, exists := seen[descriptor.Name]; exists {
			return nil, fmt.Errorf("duplicate experiment name %q", descriptor.Name)
		}
		seen[descriptor.Name] = struct{}{}
	}
	return &Registry{experiments: experiments}, nil
}

// OnFrame sends one frame to every experiment in registration order.
func (r *Registry) OnFrame(ctx context.Context, frame Frame) error {
	if r.finished {
		return ErrRegistryFinished
	}
	for _, experiment := range r.experiments {
		descriptor := experiment.Descriptor()
		if err := experiment.OnFrame(ctx, frame); err != nil {
			return fmt.Errorf("experiment %s@%s on frame: %w", descriptor.Name, descriptor.Version, err)
		}
	}
	return nil
}

// Finish finalizes every experiment once, in registration order.
func (r *Registry) Finish(ctx context.Context) ([]Result, error) {
	if r.finished {
		return nil, ErrRegistryFinished
	}
	r.finished = true

	results := make([]Result, 0, len(r.experiments))
	for _, experiment := range r.experiments {
		descriptor := experiment.Descriptor()
		result, err := experiment.Finish(ctx)
		if err != nil {
			return nil, fmt.Errorf("experiment %s@%s finish: %w", descriptor.Name, descriptor.Version, err)
		}
		if result.Descriptor != descriptor {
			return nil, fmt.Errorf(
				"experiment %s@%s returned descriptor %s@%s",
				descriptor.Name, descriptor.Version,
				result.Descriptor.Name, result.Descriptor.Version,
			)
		}
		results = append(results, result)
	}
	return results, nil
}

func isNilExperiment(experiment Experiment) bool {
	if experiment == nil {
		return true
	}
	value := reflect.ValueOf(experiment)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func validateDescriptor(descriptor Descriptor) error {
	if strings.TrimSpace(descriptor.Name) == "" {
		return errors.New("experiment name is required")
	}
	if strings.TrimSpace(descriptor.Version) == "" {
		return fmt.Errorf("experiment %q version is required", descriptor.Name)
	}
	return nil
}
