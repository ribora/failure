package failure

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
)

const (
	Kind      = "gorib_app_error_v1"
	PanicType = "gorib_panic_v1"
	unknown   = "unknown_error"
)

var (
	ErrPanic = errors.New("panic")
	registry = map[string]error{
		PanicType: ErrPanic,
	}
	mu sync.RWMutex
)

type Error struct {
	codes   []error
	message string
	payload map[string]any
	stack   []uintptr
	frames  []Frame
}

type Frame struct {
	Function string
	File     string
	Line     int
}

type aux struct {
	Kind    string         `json:"kind"`
	Type    string         `json:"type"`
	Types   []string       `json:"types"`
	Message string         `json:"message"`
	Payload map[string]any `json:"payload"`
}

func New(format string, args ...any) error {
	if format == "" {
		format = unknown
	}
	codeStr := slugify(format)
	mu.Lock()
	if _, ok := registry[codeStr]; !ok {
		registry[codeStr] = errors.New(codeStr)
	}
	mu.Unlock()
	return create([]error{registry[codeStr]}, format, args...)
}

func Wrap(format string, args ...any) error {
	var causes []error
	if !strings.HasPrefix(format, "%w") {
		if codeStr := slugify(format); codeStr != "" {
			causes = append(causes, New(format))
		}
	}
	for _, a := range args {
		if e, ok := a.(error); ok {
			causes = append(causes, e)
		}
	}
	err := create(causes, format, args...)
	err.stack = captureStack(3)
	return err
}

func Parse(err error) error {
	if err == nil {
		return nil
	}

	e, ok := FromError(err)
	if ok {
		return e
	}

	e = &Error{}
	if json.Unmarshal([]byte(err.Error()), e) == nil && len(e.codes) > 0 {
		e.stack = captureStack(3)
		return e
	}
	return Wrap("%w", err)
}

func FromBytes(b []byte) (*Error, bool) {
	err := &Error{}
	if json.Unmarshal(b, err) == nil {
		return err, true
	}
	return nil, false
}

func FromError(err error) (*Error, bool) {
	if err == nil {
		return nil, false
	}
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

func Panic(r any) error {
	if r == nil {
		return nil
	}
	err := create([]error{ErrPanic}, "panic: {panic}", fmt.Sprint(r))
	err.frames = parseDebugStack(debug.Stack(), 4)
	return err
}

func create(causes []error, format string, args ...any) *Error {
	msgWithErrors := format
	if strings.Contains(format, "%w") {
		var errArgs []any
		for _, e := range args {
			if err, ok := e.(error); ok {
				errArgs = append(errArgs, err)
			}
		}
		msgWithErrors = fmt.Errorf(format, errArgs...).Error()
	}

	combinedPayload := make(map[string]any)
	for i := len(causes) - 1; i >= 0; i-- {
		var child *Error
		if errors.As(causes[i], &child) {
			for k, v := range child.payload {
				combinedPayload[k] = v
			}
		}
	}
	finalMsg, localPayload := render(msgWithErrors, args, combinedPayload)
	for k, v := range localPayload {
		combinedPayload[k] = v
	}

	return &Error{
		codes:   causes,
		message: finalMsg,
		payload: combinedPayload,
	}
}

func render(template string, args []any, initialPayload map[string]any) (string, map[string]any) {
	var sb strings.Builder
	payload := make(map[string]any)
	for k, v := range initialPayload {
		payload[k] = v
	}
	var dataArgs []any
	for _, a := range args {
		if _, ok := a.(error); !ok {
			dataArgs = append(dataArgs, a)
		}
	}

	argIdx, i := 0, 0
	for i < len(template) {
		if template[i] == '{' {
			end := strings.IndexByte(template[i:], '}')
			if end != -1 && !strings.Contains(template[i:i+end], " ") {
				key := template[i+1 : i+end]
				if argIdx < len(dataArgs) {
					val := dataArgs[argIdx]
					payload[key] = val
					sb.WriteString(fmt.Sprint(val))
					argIdx++
					i += end + 1
					continue
				}
				if val, ok := payload[key]; ok {
					sb.WriteString(fmt.Sprint(val))
					i += end + 1
					continue
				}
			}
		}
		sb.WriteByte(template[i])
		i++
	}

	if argIdx < len(dataArgs) {
		sb.WriteString(" %!(EXTRA ")
		for j, extra := range dataArgs[argIdx:] {
			if j > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%T=%v", extra, extra))
		}
		sb.WriteString(")")
	}
	return sb.String(), payload
}

func (e *Error) Error() string           { return e.message }
func (e *Error) Unwrap() []error         { return e.codes }
func (e *Error) Payload() map[string]any { return e.payload }
func (e *Error) Stack() []Frame {
	if len(e.frames) > 0 {
		return e.frames
	}
	frames := runtime.CallersFrames(e.stack)
	var res []Frame
	for {
		frame, more := frames.Next()
		isInternal := strings.Contains(frame.File, "runtime/") ||
			strings.Contains(frame.Function, "failure.")

		if frame.Function != "" && !isInternal {
			res = append(res, Frame{
				Function: frame.Function,
				File:     frame.File,
				Line:     frame.Line,
			})
		}
		if !more {
			break
		}
	}
	return res
}
func (e *Error) Type() string {
	if len(e.codes) > 0 {
		var child *Error
		if errors.As(e.codes[0], &child) {
			return child.Type()
		}
		return slugify(e.codes[0].Error())
	}
	return unknown
}

func (e *Error) MarshalJSON() ([]byte, error) {
	types := make([]string, 0, len(e.codes))
	for _, c := range e.codes {
		var child *Error
		if errors.As(c, &child) {
			types = append(types, child.Type())
		} else {
			types = append(types, slugify(c.Error()))
		}
	}
	return json.Marshal(aux{
		Kind:    Kind,
		Type:    e.Type(),
		Types:   types,
		Message: e.message,
		Payload: e.payload,
	})
}

func (e *Error) UnmarshalJSON(data []byte) error {
	var a aux
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	if a.Kind != Kind {
		return errors.New("invalid kind")
	}

	e.message = a.Message
	e.payload = a.Payload
	mu.RLock()
	defer mu.RUnlock()

	e.codes = make([]error, len(a.Types))
	for i, t := range a.Types {
		if c, ok := registry[t]; ok {
			e.codes[i] = c
		} else {
			e.codes[i] = errors.New(t)
		}
	}
	return nil
}

func slugify(s string) string {
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

func captureStack(skip int) []uintptr {
	var pcs [32]uintptr
	n := runtime.Callers(skip, pcs[:])
	return pcs[0:n]
}

func parseDebugStack(stack []byte, skip int) (frames []Frame) {
	lines := strings.Split(string(stack), "\n")
	for i := 1; i < len(lines); i++ {
		funcName := strings.TrimSpace(lines[i])
		if funcName == "" || strings.HasPrefix(funcName, "created by") {
			continue
		}

		if idx := strings.LastIndex(funcName, "("); idx != -1 {
			funcName = funcName[:idx]
		}

		i++
		if i >= len(lines) {
			break
		}
		fileLine := strings.TrimSpace(lines[i])

		if idx := strings.LastIndex(fileLine, " +0x"); idx != -1 {
			fileLine = fileLine[:idx]
		}

		if idx := strings.LastIndex(fileLine, ":"); idx != -1 {
			file := fileLine[:idx]
			lineStr := fileLine[idx+1:]
			line, _ := strconv.Atoi(lineStr)

			frames = append(frames, Frame{
				Function: funcName,
				File:     file,
				Line:     line,
			})
		}
	}

	if len(frames) > skip {
		frames = frames[skip:]
	}
	return frames
}
