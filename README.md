# failure

Structured, typed errors for Go. Errors carry a stable type code, human-readable message, key/value payload, and optional stack trace. They serialize to JSON and survive API boundaries with type identity intact.

## Installation

```
go get gitlab.com/gorib/failure
```

## Concepts

| Concept | Description |
|---------|-------------|
| **Type code** | Stable slug derived from the format string (`"resource not found"` → `"resource_not_found"`). Used for `errors.Is` matching and JSON wire format. |
| **Message** | Human-readable string with values substituted in. |
| **Payload** | `map[string]any` of named values extracted from the message template. |
| **Stack** | Call-site frames captured by `Wrap` (not `New`). |

## Usage

### Sentinel errors

Declare typed sentinels at package level with `New`:

```go
var (
    ErrNotFound   = failure.New("resource not found")
    ErrForbidden  = failure.New("forbidden")
)
```

The format string becomes the type code. Two calls with the same format string share the same type code, so `errors.Is` works across package boundaries.

### Call-site errors

Use `Wrap` at the point of failure. It captures a stack trace and associates causes:

```go
func fetchUser(id string) error {
    return failure.Wrap("%w: id={id}", ErrNotFound, id)
}
```

`Wrap` captures the stack at the call site. `New` does not — it is for sentinels only.

### Message templates

Two substitution forms are supported:

| Syntax | Substitutes | Stored in payload |
|--------|-------------|-------------------|
| `%w`   | Wrapped error's message (standard library) | No |
| `{key}` | Next non-error argument | Yes, under `"key"` |

```go
err := failure.Wrap("db error: table={table} op={op}", ErrDB, "users", "insert")
// err.Error()   → "db error: table=users op=insert"
// err.Payload() → map[string]any{"table": "users", "op": "insert"}
```

You can combine both:

```go
err := failure.Wrap("%w: user={user}", cause, username)
```

### Checking error types

```go
if errors.Is(err, ErrNotFound) {
    // type codes match
}

if e, ok := failure.FromError(err); ok {
    fmt.Println(e.Type())    // "resource_not_found"
    fmt.Println(e.Payload()) // map[string]any{...}
}
```

### Stack traces

```go
if e, ok := failure.FromError(err); ok {
    for _, f := range e.Stack() {
        fmt.Printf("%s\n\t%s:%d\n", f.Function, f.File, f.Line)
    }
}
```

### Panics

```go
defer func() {
    if r := recover(); r != nil {
        err = failure.Panic(r)
        // errors.Is(err, failure.ErrPanic) == true
    }
}()
```

### JSON serialization

`*Error` implements `json.Marshaler` / `json.Unmarshaler`.

Wire format:

```json
{
  "kind":    "gorib_app_error_v1",
  "type":    "resource_not_found",
  "types":   ["resource_not_found"],
  "message": "resource not found: id=42",
  "payload": {"id": "42"}
}
```

Decoding on the receiving side:

```go
// From raw bytes
e, ok := failure.FromBytes(responseBody)

// From any error (tries type assertion → JSON → wrap)
err = failure.Parse(err)
```

After decoding, `errors.Is(e, ErrNotFound)` works provided the sentinel was registered in the current process (i.e. `failure.New("resource not found")` was called). If the type is unrecognized, the type code string is used for matching.

## API reference

```go
// Creation
func New(format string, args ...any) error
func Wrap(format string, args ...any) error
func Panic(r any) error

// Inspection
func FromError(err error) (*Error, bool)
func FromBytes(b []byte) (*Error, bool)
func Parse(err error) error

// Error methods
func (e *Error) Type() string
func (e *Error) Payload() map[string]any
func (e *Error) Stack() []Frame
func (e *Error) Error() string
func (e *Error) Unwrap() []error

// Sentinels
var ErrPanic = failure.New("panic")
```
