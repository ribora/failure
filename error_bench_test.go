package failure_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ribora/failure"
)

var (
	errStd  = errors.New("no rows in result set")
	errFailure = failure.New("no rows in result set")

	sink  error
	match bool
)

func BenchmarkStdErrorf(b *testing.B) {
	for b.Loop() {
		sink = fmt.Errorf("%w: id=%s", errStd, "42")
	}
}

func BenchmarkWrap(b *testing.B) {
	for b.Loop() {
		sink = failure.Wrap("%w: id={id}", errFailure, "42")
	}
}

func BenchmarkNew(b *testing.B) {
	for b.Loop() {
		sink = failure.New("no rows in result set")
	}
}

func BenchmarkStdIs(b *testing.B) {
	err := fmt.Errorf("%w: id=%s", errStd, "42")
	for b.Loop() {
		match = errors.Is(err, errStd)
	}
}

func BenchmarkIs(b *testing.B) {
	err := failure.Wrap("%w: id={id}", errFailure, "42")
	for b.Loop() {
		match = errors.Is(err, errFailure)
	}
}
