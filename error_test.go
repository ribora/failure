package failure_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"gitlab.com/gorib/failure"
)

const wire = `{"kind":"gorib_app_error_v1","type":"gateway_error","types":["gateway_error"],"message":"gateway error: oops","payload":{"message":"oops"}}`

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
