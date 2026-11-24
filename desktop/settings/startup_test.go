package settings

import (
	"errors"
	"testing"
)

type registration struct {
	enabled bool
	err     error
}

func (r *registration) IsEnabled() (bool, error) { return r.enabled, nil }
func (r *registration) Enable() error {
	if r.err == nil {
		r.enabled = true
	}
	return r.err
}
func (r *registration) Disable() error {
	if r.err == nil {
		r.enabled = false
	}
	return r.err
}

func TestStartup(t *testing.T) {
	manager := &registration{}
	service := Startup{Manager: manager}
	for _, want := range []bool{true, true, false, false} {
		got, err := service.SetEnabled(want)
		if err != nil || got != want || manager.enabled != want {
			t.Fatalf("SetEnabled(%v) returned %v, %v", want, got, err)
		}
	}
	manager.err = errors.New("access denied")
	if _, err := service.SetEnabled(true); !errors.Is(err, manager.err) {
		t.Fatalf("registration error lost: %v", err)
	}
}
