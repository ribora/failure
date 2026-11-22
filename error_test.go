package failure_test

import (
	"encoding/json"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/ribora/failure"
)

const wire = `{"kind":"ribora_app_error_v1","type":"gateway_error","types":["gateway_error"],"message":"gateway error: oops","payload":{"message":"oops"}}`

const wireLegacy = `{"kind":"gorib_app_error_v1","type":"gateway_error","types":["gateway_error"],"message":"gateway error: oops","payload":{"message":"oops"}}`

func TestSameTextIsTheSameError(t *testing.T) {
	one := failure.New("oops")
	two := failure.New("oops")
	if !errors.Is(one, two) {
		t.Fatal("two declarations of the same text are one error, and nothing else here would work")
	}

	plainOne := errors.New("oops")
	plainTwo := errors.New("oops")
	if errors.Is(plainOne, plainTwo) {
		t.Fatal("unlike stdlib errors, which are the same only when they are the same object")
	}
}

func TestAnErrorTravelsAsItsTypeMessageAndPayload(t *testing.T) {
	err := failure.Wrap("%w: {message}", failure.New("gateway error"), "oops")

	data, mErr := json.Marshal(err)
	if mErr != nil {
		t.Fatal(mErr)
	}
	if string(data) != wire {
		t.Fatalf("on the wire:\n got %s\nwant %s", data, wire)
	}
}

func TestAnErrorReadOffTheWireIsTheSentinelDeclaredHere(t *testing.T) {
	declaredHere := failure.New("gateway error")

	err, ok := failure.FromBytes([]byte(wire))
	if !ok {
		t.Fatal("unreadable")
	}
	if !errors.Is(err, declaredHere) {
		t.Fatal("a neighbour's refusal is matched by what we declare, or no caller could act on one")
	}
	if got := err.Type(); got != "gateway_error" {
		t.Fatalf("type = %q", got)
	}
	if got := err.Payload()["message"]; got != "oops" {
		t.Fatalf("payload message = %v, want oops", got)
	}
}

func TestAnErrorFromAServiceStillOnGoribIsReadTheSameWay(t *testing.T) {
	declaredHere := failure.New("gateway error")

	err, ok := failure.FromBytes([]byte(wireLegacy))
	if !ok {
		t.Fatal("unreadable")
	}
	if !errors.Is(err, declaredHere) {
		t.Fatal("a service that has not moved yet still speaks the old kind")
	}
	if got := err.Payload()["message"]; got != "oops" {
		t.Fatalf("payload message = %v, want oops", got)
	}
}

func TestAPlaceholderGoesToBothMessageAndPayload(t *testing.T) {
	err := failure.Wrap("%w: {message}", failure.New("gateway error"), "oops")

	if err.Error() != "gateway error: oops" {
		t.Fatalf("message = %q", err.Error())
	}
	fe, _ := failure.FromError(err)
	if got := fe.Payload()["message"]; got != "oops" {
		t.Fatalf("payload message = %v, want oops", got)
	}
}

// %w hands the identity to what is wrapped, and the text around it is only text
func TestWhereTheTypeComesFrom(t *testing.T) {
	wrapped := failure.Wrap("oops: %w", errors.New("oops i did it again"))
	fe, _ := failure.FromError(wrapped)
	if got := fe.Type(); got != "oops_i_did_it_again" {
		t.Fatalf("type = %q, want the wrapped error's", got)
	}

	written := failure.Wrap("oops i did it again")
	fe, _ = failure.FromError(written)
	if got := fe.Type(); got != "oops_i_did_it_again" {
		t.Fatalf("type = %q, want the format's", got)
	}
}

// the gRPC status interceptor runs one Is per entry of its error mapper, on every
// failed call
func TestIsDoesNotAllocate(t *testing.T) {
	sentinel := failure.New("no rows in result set")
	err := failure.Wrap("%w: id={id}", sentinel, "42")

	var match bool
	if n := testing.AllocsPerRun(100, func() { match = errors.Is(err, sentinel) }); n != 0 {
		t.Fatalf("errors.Is allocates %v times per call", n)
	}
	if !match {
		t.Fatal("precondition: the sentinel matches")
	}
}

func TestSlugifyTakesTheShortcutOnlyWhenItChangesNothing(t *testing.T) {
	longWay := func(s string) string {
		s = strings.ReplaceAll(s, "%w", "")
		var sb strings.Builder
		inBr := false
		for _, r := range s {
			if r == '{' {
				inBr = true
				continue
			}
			if r == '}' {
				inBr = false
				continue
			}
			if !inBr {
				if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
					sb.WriteRune(r)
				} else if sb.Len() > 0 && !strings.HasSuffix(sb.String(), "_") {
					sb.WriteRune('_')
				}
			}
		}
		return strings.ToLower(strings.Trim(sb.String(), "_"))
	}

	for _, s := range []string{
		"", "_", "__", "_a", "a_", "a__b", "a_b", "abc", "a1_b2", "0",
		"no rows in result set", "no_rows_in_result_set",
		"%w: id={id}", "UPPER", "404 Not Found", "привет",
	} {
		if got, want := failure.Slugify(s), longWay(s); got != want {
			t.Fatalf("Slugify(%q) = %q, the long way round gives %q", s, got, want)
		}
	}

	alphabet := []rune("abcXYZ019_ -.:{}%wпр")
	r := rand.New(rand.NewPCG(1, 2))
	for range 20000 {
		var sb strings.Builder
		for range r.IntN(24) {
			sb.WriteRune(alphabet[r.IntN(len(alphabet))])
		}
		s := sb.String()
		if got, want := failure.Slugify(s), longWay(s); got != want {
			t.Fatalf("Slugify(%q) = %q, the long way round gives %q", s, got, want)
		}
	}
}

type ValidationError struct {
	Tag string `json:"tag"`
	Val string `json:"val"`
}

func (e *ValidationError) Error() string {
	return e.Tag + ": " + e.Val
}

type Errors []ValidationError

func (e Errors) String() string {
	msg := make([]string, 0, len(e))
	for _, err := range e {
		msg = append(msg, err.Error())
	}
	return strings.Join(msg, "; ")
}

func TestAPlaceholderKeepsItsShapeInThePayload(t *testing.T) {
	errs := Errors{{Tag: "email", Val: "requred"}, {Tag: "name", Val: "min"}}

	err := failure.Wrap("%w: {errs}", failure.New("oops"), errs)

	if err.Error() != "oops: email: requred; name: min" {
		t.Fatalf("message = %q, want the value rendered by String", err.Error())
	}
	data, mErr := json.Marshal(err)
	if mErr != nil {
		t.Fatal(mErr)
	}
	const want = `"payload":{"errs":[{"tag":"email","val":"requred"},{"tag":"name","val":"min"}]}`
	if !strings.Contains(string(data), want) {
		t.Fatalf("payload flattened to text:\n%s", data)
	}
}
