package config

import (
	"context"
	"testing"
)

type fakeConfigurer struct {
	cfg         Config
	retrieveErr error
	saveErr     error
}

func (f *fakeConfigurer) Retrieve(context.Context) (Config, error) {
	if f.retrieveErr != nil {
		return Config{}, f.retrieveErr
	}
	return f.cfg, nil
}

func (f *fakeConfigurer) Save(context.Context, Config) error {
	return f.saveErr
}

func TestFromContext_errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ctx  context.Context
	}{
		{"background", context.Background()},
		{"wrong type", context.WithValue(context.Background(), ctxKey{}, "not an Interface")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := FromContext(tt.ctx)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestIntoContext_FromContext_roundTrip(t *testing.T) {
	t.Parallel()
	want := &fakeConfigurer{cfg: Config{KBs: map[Unique]KB{}}}
	ctx := IntoContext(context.Background(), want)
	got, err := FromContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal("FromContext returned different instance")
	}
}

func TestIntoContext_FromContext_nilValue(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(context.Background(), ctxKey{}, nil)
	_, err := FromContext(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
}
