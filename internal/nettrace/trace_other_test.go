//go:build !linux

package nettrace

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"testing"
)

func TestSupportedOffLinux(t *testing.T) {
	if err := Supported(); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Supported() = %v, want ErrUnsupported", err)
	}
}

func TestTraceUnsupportedOffLinux(t *testing.T) {
	res, err := Trace(context.Background(), netip.MustParseAddr("192.0.2.1"), Options{})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
	if !reflect.DeepEqual(res, Result{}) {
		t.Errorf("result = %+v, want the empty Result", res)
	}
}
