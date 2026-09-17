package failure_test

import (
	"errors"
	"fmt"
	"testing"

	"gitlab.com/gorib/failure"
)

var (
	stdSentinel  = errors.New("no rows in result set")
	failSentinel = failure.New("no rows in result set")

	sink  error
	match bool
)

func BenchmarkStdErrorf(b *testing.B) {
	for b.Loop() {
		sink = fmt.Errorf("%w: id=%s", stdSentinel, "42")
	}
}

func BenchmarkWrap(b *testing.B) {
	for b.Loop() {
		sink = failure.Wrap("%w: id={id}", failSentinel, "42")
	}
}

func BenchmarkNew(b *testing.B) {
	for b.Loop() {
		sink = failure.New("no rows in result set")
	}
}

func BenchmarkStdIs(b *testing.B) {
	err := fmt.Errorf("%w: id=%s", stdSentinel, "42")
	for b.Loop() {
		match = errors.Is(err, stdSentinel)
	}
}

func BenchmarkIs(b *testing.B) {
	err := failure.Wrap("%w: id={id}", failSentinel, "42")
	for b.Loop() {
		match = errors.Is(err, failSentinel)
	}
}
