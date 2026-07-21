package experiments

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeExperiment struct {
	descriptor Descriptor
	calls      *[]string
	onFrameErr error
	finishErr  error
	result     Result
}

func (f *fakeExperiment) Descriptor() Descriptor { return f.descriptor }

func (f *fakeExperiment) OnFrame(context.Context, Frame) error {
	*f.calls = append(*f.calls, "frame:"+f.descriptor.Name)
	return f.onFrameErr
}

func (f *fakeExperiment) Finish(context.Context) (Result, error) {
	*f.calls = append(*f.calls, "finish:"+f.descriptor.Name)
	return f.result, f.finishErr
}

func TestNewRegistryRejectsInvalidExperiments(t *testing.T) {
	var typedNil *fakeExperiment
	tests := []struct {
		name  string
		items []Experiment
		want  string
	}{
		{name: "nil", items: []Experiment{nil}, want: ErrNilExperiment.Error()},
		{name: "typed nil", items: []Experiment{typedNil}, want: ErrNilExperiment.Error()},
		{name: "blank name", items: []Experiment{newFake(Descriptor{Version: "v1"}, nil)}, want: "name is required"},
		{name: "blank version", items: []Experiment{newFake(Descriptor{Name: "shape"}, nil)}, want: "version is required"},
		{name: "duplicate name", items: []Experiment{
			newFake(Descriptor{Name: "shape", Version: "v1"}, nil),
			newFake(Descriptor{Name: "shape", Version: "v2"}, nil),
		}, want: "duplicate experiment name"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewRegistry(test.items...)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewRegistry() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestRegistryPreservesLifecycleOrder(t *testing.T) {
	calls := make([]string, 0, 4)
	first := newFake(Descriptor{Name: "first", Version: "v1"}, &calls)
	second := newFake(Descriptor{Name: "second", Version: "v2"}, &calls)
	registry, err := NewRegistry(first, second)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.OnFrame(context.Background(), Frame{}); err != nil {
		t.Fatal(err)
	}
	results, err := registry.Finish(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := calls, []string{"frame:first", "frame:second", "finish:first", "finish:second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if got, want := []Descriptor{results[0].Descriptor, results[1].Descriptor}, []Descriptor{first.descriptor, second.descriptor}; !reflect.DeepEqual(got, want) {
		t.Fatalf("descriptors = %v, want %v", got, want)
	}
}

func TestRegistryWrapsExperimentErrors(t *testing.T) {
	calls := make([]string, 0, 1)
	cause := errors.New("failed replay")
	experiment := newFake(Descriptor{Name: "shape", Version: "v3"}, &calls)
	experiment.onFrameErr = cause
	registry, err := NewRegistry(experiment)
	if err != nil {
		t.Fatal(err)
	}
	err = registry.OnFrame(context.Background(), Frame{})
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "shape@v3 on frame") {
		t.Fatalf("OnFrame() error = %v", err)
	}
}

func TestRegistryClosesLifecycleAfterFinishAttempt(t *testing.T) {
	calls := make([]string, 0, 1)
	experiment := newFake(Descriptor{Name: "shape", Version: "v1"}, &calls)
	registry, err := NewRegistry(experiment)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Finish(context.Background()); !errors.Is(err, ErrRegistryFinished) {
		t.Fatalf("second Finish() error = %v, want %v", err, ErrRegistryFinished)
	}
	if err := registry.OnFrame(context.Background(), Frame{}); !errors.Is(err, ErrRegistryFinished) {
		t.Fatalf("OnFrame() after Finish error = %v, want %v", err, ErrRegistryFinished)
	}
}

func TestRegistryRejectsMismatchedResultDescriptor(t *testing.T) {
	calls := make([]string, 0, 1)
	experiment := newFake(Descriptor{Name: "shape", Version: "v1"}, &calls)
	experiment.result.Descriptor.Version = "v2"
	registry, err := NewRegistry(experiment)
	if err != nil {
		t.Fatal(err)
	}
	_, err = registry.Finish(context.Background())
	if err == nil || !strings.Contains(err.Error(), "returned descriptor shape@v2") {
		t.Fatalf("Finish() error = %v", err)
	}
}

func newFake(descriptor Descriptor, calls *[]string) *fakeExperiment {
	if calls == nil {
		calls = &[]string{}
	}
	return &fakeExperiment{
		descriptor: descriptor,
		calls:      calls,
		result:     Result{Descriptor: descriptor},
	}
}
