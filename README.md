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
| **Transport** | The status of the one hop the error crossed — an HTTP response, a gRPC status. Reachable, but not part of the error's identity, and removable before the error is raised onward. |

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

Matching compares type codes, never pointers. Against a target that is not a `failure` — a stdlib sentinel, one from somebody else's library — the target's own message is slugified and compared the same way, so `errors.Is(err, errors.New("resource not found"))` matches too.

`Slugify` is exported because that rule has to be applied outside as well, and it is what turns a message into a type code: `%w` is removed, `{placeholder}` names are dropped along with their braces, letters and digits are kept, everything else collapses into single underscores, and the result is lowercased and trimmed. A slug slugifies to itself.

```go
failure.Slugify("db error: table={table} op={op}") // "db_error_table_op"
```

### Stack traces

```go
if e, ok := failure.FromError(err); ok {
    for _, f := range e.Stack() {
        fmt.Printf("%s\n\t%s:%d\n", f.Function, f.File, f.Line)
    }
}
```

Frames come newest first, with the runtime's own and `failure`'s own left out. A sink that wants them oldest first — Sentry does — reverses the slice itself.

```go
type Frame struct {
    Function string
    File     string
    Line     int
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

`Catch` is the same thing around a call, which is what a middleware wants:

```go
err := failure.Catch(func() error { return handler(ctx) })
```

An error the function returned itself wins — `Catch` only fills in the panic when the call came back nil.

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

`kind` is the discriminator, exported as `failure.Kind`. Decoding refuses a document carrying any other kind, so an unrelated JSON body that happens to have a `type` and a `message` is not taken for one of these. gRPC carries the same constant as the `ErrorInfo` domain, which is how an interceptor recognises the detail as ours.

Decoding on the receiving side:

```go
// From raw bytes
e, ok := failure.FromBytes(responseBody)

// From any error (tries type assertion → JSON → wrap)
err = failure.Parse(err)
```

After decoding, `errors.Is(e, ErrNotFound)` works provided the sentinel was registered in the current process (i.e. `failure.New("resource not found")` was called). If the type is unrecognized, the type code string is used for matching.

### Transport

An error read off a neighbour's answer arrives with the status of the hop it crossed. That status is the neighbour's answer, not yours: fold it into the cause chain and it travels onward, until some service down the line renders a neighbour's 404 as its own.

`AttachTransport` puts it in a slot of its own instead. A client attaches what it just crossed:

```go
answer, ok := failure.FromBytes(respBody)
if !ok {
    return failure.AttachTransport(failure.New(resp.Status), httpErr)
}
return failure.AttachTransport(answer, httpErr)
```

Whoever answers for the error strips it; whoever forwards it leaves it alone:

```go
failure.Transport(err)       // the status it crossed, or nil
failure.DetachTransport(err) // the same error, with every hop's status removed
```

- Identity is untouched: `Type()` and `errors.Is` answer the error's own code, never the transport's.
- `errors.As` still reaches it — `Unwrap` yields the transport — so a caller that forwards can read the status.
- `DetachTransport` is recursive: a status an earlier hop left behind goes with it.
- Attaching twice replaces. One hop, one status.
- Attaching to a plain error turns it into a failure, typed by the slug of its message.
- Attaching captures a stack if the error has none — a sentinel from `New`, or one decoded off the wire.
- Transport is not serialized. The wire format above has no field for it, so each hop attaches its own.

### Answering for a neighbour

An adapter that answers for its neighbour turns the neighbour's refusal into one of its own:

```go
var priceErrors = failure.NewTranslation(model.ErrInfrastructure, map[error]error{
    failure.New("promo code is not valid"):     model.ErrPromoNotApplicable,
    failure.New("payment method is not found"): nil,
})

resp, err := client.Quote(ctx, req)
if err != nil {
    return 0, priceErrors.Translate(err)
}
```

- The table is matched against the error's own type, by slug. The neighbour's status code is not the contract, and neither is a code wrapped under its type.
- A named code becomes the mapped error, anything else the fallback. Either way the neighbour's message and payload are kept for whoever reads the log, but its type and transport are not: `errors.Is` answers the caller's error only, and the caller's own status answers.
- A `nil` value means the answer is not an error: `Translate` returns nil and the caller returns its zero value.
- A nil fallback forwards anything the table does not name: the error comes back as it came, transport included.
- Two keys with one slug are one key, and which of their values wins is not defined.

## API reference

```go
// Creation
func New(format string, args ...any) error
func Wrap(format string, args ...any) error
func Panic(r any) error
func Catch(fn func() error) error

// Inspection
func FromError(err error) (*Error, bool)
func FromBytes(b []byte) (*Error, bool)
func Parse(err error) error

// Transport
func AttachTransport(err error, transport error) error
func Transport(err error) error
func DetachTransport(err error) error

// Translation
func NewTranslation(fallback error, known map[error]error) Translation
func (t Translation) Translate(err error) error

// Error methods
func (e *Error) Type() string
func (e *Error) Payload() map[string]any
func (e *Error) Stack() []Frame
func (e *Error) Error() string
func (e *Error) Unwrap() []error
func (e *Error) Is(target error) bool

// Type codes
func Slugify(s string) string
const Kind = "gorib_app_error_v1"

// Sentinels
var ErrPanic = errors.New("panic")
```
