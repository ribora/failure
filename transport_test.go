package failure_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	"gitlab.com/gorib/failure"
)

// stands for a grpc status or an http error: a foreign type a caller reads an answer out of
type transport struct{ code int }

func (t *transport) Error() string { return fmt.Sprintf("transport %d", t.code) }

var errRefused = failure.New("promo code is not valid")

func carried() (error, *transport) {
	pipe := &transport{code: 400}
	return failure.AttachTransport(failure.Wrap("%w: {code}", errRefused, "SALE"), pipe), pipe
}

func TestAttachedTransportIsFoundByAs(t *testing.T) {
	err, pipe := carried()

	var found *transport
	if !errors.As(err, &found) {
		t.Fatal("a forwarding caller must still find the transport")
	}
	if found != pipe {
		t.Fatalf("found %v, want %v", found, pipe)
	}
}

func TestAttachedTransportIsNotTheIdentity(t *testing.T) {
	err, _ := carried()

	fe, ok := failure.FromError(err)
	if !ok {
		t.Fatal("not a failure")
	}
	if got := fe.Type(); got != "promo_code_is_not_valid" {
		t.Fatalf("type = %q, want the error's own", got)
	}
	if !errors.Is(err, errRefused) {
		t.Fatal("the error still says what it said")
	}
	if got := fe.Payload()["code"]; got != "SALE" {
		t.Fatalf("payload code = %v, want SALE", got)
	}
}

func TestAttachedTransportDoesNotTravel(t *testing.T) {
	err, _ := carried()

	data, mErr := json.Marshal(err)
	if mErr != nil {
		t.Fatal(mErr)
	}
	var out struct {
		Types []string `json:"types"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(out.Types, "transport_400") {
		t.Fatalf("types = %v, the transport must not travel", out.Types)
	}
	if !slices.Contains(out.Types, "promo_code_is_not_valid") {
		t.Fatalf("types = %v, want the error's own", out.Types)
	}
}

func TestDetachRemovesIt(t *testing.T) {
	err, _ := carried()

	got := failure.DetachTransport(err)

	var found *transport
	if errors.As(got, &found) {
		t.Fatal("the transport must not answer for a caller that answers for itself")
	}
	if !errors.Is(got, errRefused) {
		t.Fatal("what was said survives")
	}
	fe, _ := failure.FromError(got)
	if fe.Payload()["code"] != "SALE" {
		t.Fatal("payload survives")
	}
	if fe.Error() != "promo code is not valid: SALE" {
		t.Fatalf("message = %q", fe.Error())
	}
}

func TestDetachIsRecursive(t *testing.T) {
	carriedErr, _ := carried()
	nested := failure.Wrap("%w: %w", failure.New("could not price the line"), carriedErr)

	var found *transport
	if !errors.As(nested, &found) {
		t.Fatal("precondition: the nested transport is reachable")
	}

	got := failure.DetachTransport(nested)
	if errors.As(got, &found) {
		t.Fatal("a transport of an earlier hop means nothing to the next reader")
	}
	if !errors.Is(got, errRefused) {
		t.Fatal("what was said survives the nesting")
	}
}

func TestDetachKeepsWhatIsNotTransport(t *testing.T) {
	// whisk builds this shape: a foreign error carried as content, read by type upstream
	reason := errors.New("mail: no address")
	err := failure.Wrap("%w: %w", failure.New("invalid payload"), reason)

	got := failure.DetachTransport(err)
	if !errors.Is(got, reason) {
		t.Fatal("a foreign cause that was never marked is content and stays")
	}
}

func TestAttachTakesATrace(t *testing.T) {
	// what the interceptors hand it: New has no trace of its own, and neither has an error
	// read off the wire
	for name, err := range map[string]error{
		"built here":      failure.New("404 Not Found"),
		"read off a wire": fromWire(t),
		"not ours":        errors.New("dial tcp: connection refused"),
	} {
		t.Run(name, func(t *testing.T) {
			fe, ok := failure.FromError(failure.AttachTransport(err, &transport{code: 503}))
			if !ok {
				t.Fatal("not a failure")
			}
			if len(fe.Stack()) == 0 {
				t.Fatal("no trace, and this is the last place one could be had")
			}
		})
	}

	kept := failure.Wrap("%w: {code}", errRefused, "SALE")
	before, _ := failure.FromError(kept)
	after, _ := failure.FromError(failure.AttachTransport(kept, &transport{code: 503}))
	if after.Stack()[0] != before.Stack()[0] {
		t.Fatal("a trace of its own is left alone")
	}
}

func fromWire(t *testing.T) error {
	t.Helper()
	data, err := json.Marshal(failure.Wrap("%w: {code}", errRefused, "SALE"))
	if err != nil {
		t.Fatal(err)
	}
	out, ok := failure.FromBytes(data)
	if !ok {
		t.Fatal("unreadable")
	}
	return out
}

func TestTransportReads(t *testing.T) {
	err, pipe := carried()
	if got := failure.Transport(err); got != pipe {
		t.Fatalf("got %v, want the marked cause", got)
	}

	nested := failure.Wrap("%w: %w", failure.New("could not price the line"), err)
	if got := failure.Transport(nested); got != pipe {
		t.Fatalf("got %v, want the one an earlier hop left", got)
	}

	clean := failure.Wrap("%w: {code}", errRefused, "SALE")
	if got := failure.Transport(clean); got != nil {
		t.Fatalf("got %v, want nil for an error answered for", got)
	}
	if got := failure.Transport(errors.New("plain")); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestTransportDoesNotCrossTheWire(t *testing.T) {
	err, _ := carried()

	data, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	out, ok := failure.FromBytes(data)
	if !ok {
		t.Fatal("unreadable")
	}

	if got := failure.Transport(out); got != nil {
		t.Fatalf("got %v, want nil: the hop that reads this attaches its own", got)
	}
	if !errors.Is(out, errRefused) {
		t.Fatal("precondition: what was said crossed the wire")
	}
}

type carrier struct{ err error }

func (c *carrier) Error() string { return "carried: " + c.err.Error() }
func (c *carrier) Unwrap() error { return c.err }

func TestDetachKeepsAForeignWrapperItself(t *testing.T) {
	wrapper := &carrier{err: failure.Wrap("%w: {code}", errRefused, "SALE")}
	err := failure.AttachTransport(
		failure.Wrap("%w: %w", failure.New("could not send"), wrapper),
		&transport{code: 400},
	)

	var found *carrier
	if !errors.As(failure.DetachTransport(err), &found) {
		t.Fatal("a foreign node cannot be rebuilt, so it is kept whole, not swapped for what it wraps")
	}
}

func TestDetachLeavesACleanErrorAlone(t *testing.T) {
	flat := failure.Wrap("%w: {code}", errRefused, "SALE")
	nested := failure.Wrap("%w: %w", failure.New("could not price the line"), flat)

	for name, err := range map[string]error{"flat": flat, "nested": nested} {
		t.Run(name, func(t *testing.T) {
			if got := failure.DetachTransport(err); got != err {
				t.Fatal("nothing to remove, so nothing is copied")
			}
		})
	}
}

func TestAttachTwiceReplaces(t *testing.T) {
	first := &transport{code: 400}
	second := &transport{code: 503}
	err := failure.AttachTransport(failure.AttachTransport(failure.Parse(errRefused), first), second)

	var found *transport
	if !errors.As(err, &found) || found != second {
		t.Fatalf("found %v, want the last attached", found)
	}
	if errors.As(failure.DetachTransport(err), &found) {
		t.Fatal("no transport is left behind by a replace")
	}
}

func TestAttachOnAPlainError(t *testing.T) {
	err := failure.AttachTransport(errors.New("dial tcp: connection refused"), &transport{code: 503})

	fe, ok := failure.FromError(err)
	if !ok {
		t.Fatal("a plain error becomes a failure that can carry one")
	}
	if got := fe.Type(); got != "dial_tcp_connection_refused" {
		t.Fatalf("type = %q", got)
	}
	var found *transport
	if !errors.As(err, &found) {
		t.Fatal("the transport is reachable")
	}
	if errors.As(failure.DetachTransport(err), &found) {
		t.Fatal("and removable")
	}
}
