package failure_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ribora/failure"
)

var (
	errSkippable = failure.New("payment method is not found")
	errMine      = failure.New("promo code does not apply to this order")
	errInfra     = failure.New("infrastructure error")

	neighbour = failure.NewTranslation(errInfra, map[error]error{
		errRefused:   errMine,
		errSkippable: nil,
	})
)

func TestTranslateNil(t *testing.T) {
	if err := neighbour.Translate(nil); err != nil {
		t.Fatalf("got %v, want nil", err)
	}
}

func TestTranslateKnownRefusal(t *testing.T) {
	_, err := carried()
	got := neighbour.Translate(err)

	if !errors.Is(got, errMine) {
		t.Fatalf("got %v, want the caller's own refusal", got)
	}
	if errors.Is(got, errRefused) || errors.Is(got, errInfra) {
		t.Fatal("the neighbour's code and the fallback are both gone")
	}
	if _, ok := errors.AsType[*transport](got); ok {
		t.Fatal("the neighbour's status must not answer for the caller")
	}
	fe, _ := failure.FromError(got)
	if fe.Error() != "promo code does not apply to this order: promo code is not valid: SALE" {
		t.Fatalf("message = %q, want the neighbour's kept for the log", fe.Error())
	}
	if fe.Payload()["code"] != "SALE" {
		t.Fatalf("payload = %v, want the neighbour's kept for the log", fe.Payload())
	}
}

func TestTranslateReadOffTheWire(t *testing.T) {
	got := neighbour.Translate(failure.AttachTransport(fromWire(t), &transport{code: 400}))
	if !errors.Is(got, errMine) {
		t.Fatalf("got %v, want the caller's own refusal", got)
	}
}

func TestTranslateNilEntry(t *testing.T) {
	if err := neighbour.Translate(failure.AttachTransport(errSkippable, &transport{code: 404})); err != nil {
		t.Fatalf("got %v, want nil: the answer is not an error", err)
	}
}

func TestTranslateFallback(t *testing.T) {
	_, err := carried()
	got := failure.NewTranslation(errInfra, nil).Translate(err)

	if !errors.Is(got, errInfra) {
		t.Fatalf("got %v, want the fallback", got)
	}
	if errors.Is(got, errRefused) {
		t.Fatal("the neighbour's type must not answer for the caller")
	}
	var found *transport
	if errors.As(got, &found) {
		t.Fatal("the neighbour's status must not answer for the caller")
	}
	fe, _ := failure.FromError(got)
	if fe.Type() != "infrastructure_error" {
		t.Fatalf("type = %q", fe.Type())
	}
	if fe.Error() != "infrastructure error: promo code is not valid: SALE" {
		t.Fatalf("message = %q, want the neighbour's kept for the log", fe.Error())
	}
	if fe.Payload()["code"] != "SALE" {
		t.Fatalf("payload = %v, want the neighbour's kept for the log", fe.Payload())
	}
}

func TestTranslateMatchesOwnTypeOnly(t *testing.T) {
	nested := failure.Wrap("%w: %w", failure.New("could not price the line"), errRefused)
	if got := neighbour.Translate(nested); !errors.Is(got, errInfra) {
		t.Fatalf("got %v, want the fallback: a code under another is not the neighbour's answer", got)
	}
}

func TestTranslateTraceStartsAtTheCaller(t *testing.T) {
	_, carriedErr := carried()
	for name, got := range map[string]error{
		"known":    neighbour.Translate(carriedErr),
		"fallback": failure.NewTranslation(errInfra, nil).Translate(carriedErr),
	} {
		t.Run(name, func(t *testing.T) {
			fe, _ := failure.FromError(got)
			if top := fe.Stack()[0].Function; !strings.HasSuffix(top, "TestTranslateTraceStartsAtTheCaller") {
				t.Fatalf("top frame = %s, want the caller of Translate", top)
			}
		})
	}
}

func TestTranslateWithoutFallbackForwards(t *testing.T) {
	pipe, err := carried()
	got := failure.NewTranslation(nil, map[error]error{errSkippable: nil}).Translate(err)

	if got != err {
		t.Fatalf("got %v, want the error as it came", got)
	}
	var found *transport
	if !errors.As(got, &found) || found != pipe {
		t.Fatal("a caller that forwards keeps the neighbour's status")
	}
}

func TestTranslateWithoutFallbackStillAnswersTheTable(t *testing.T) {
	_, err := carried()
	forwarder := failure.NewTranslation(nil, map[error]error{errRefused: errMine, errSkippable: nil})

	if got := forwarder.Translate(err); !errors.Is(got, errMine) || errors.Is(got, errRefused) {
		t.Fatalf("got %v, want the caller's own refusal", got)
	}
	if got := forwarder.Translate(failure.AttachTransport(errSkippable, &transport{code: 404})); got != nil {
		t.Fatalf("got %v, want nil: the answer is not an error", got)
	}
}
