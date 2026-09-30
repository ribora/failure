package failure

import (
	"errors"
	"maps"
)

type Translation struct {
	fallback error
	known    map[string]error
}

func NewTranslation(fallback error, known map[error]error) Translation {
	bySlug := make(map[string]error, len(known))
	for code, domain := range known {
		bySlug[slugOf(code)] = domain
	}
	return Translation{fallback: fallback, known: bySlug}
}

func (t Translation) Translate(err error) error {
	if err == nil {
		return nil
	}
	answer, ok := t.known[slugOf(err)]
	if !ok {
		if t.fallback == nil {
			return err
		}
		answer = t.fallback
	}
	if answer == nil {
		return nil
	}

	payload := make(map[string]any)
	for _, e := range []error{err, answer} {
		if fe, ok := errors.AsType[*Error](e); ok {
			maps.Copy(payload, fe.payload)
		}
	}
	return &Error{
		codes:   []error{answer},
		typ:     slugOf(answer),
		message: answer.Error() + ": " + err.Error(),
		payload: payload,
		stack:   captureStack(3),
	}
}
