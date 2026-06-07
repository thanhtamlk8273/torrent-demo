# Error sentinel split

Currently all errors in `internal/bencode/decode.go` wrap a single sentinel `ErrMalformed`.
The goal is to split it into specific sentinels so callers can distinguish failure reasons
using `errors.Is` without parsing message strings.

---

## 1. Define the sentinels in `decode.go`

Replace:
```go
var ErrMalformed = errors.New("malformed bencode")
```

With:
```go
var (
    ErrUnknownType = errors.New("unknown type byte")
    ErrTruncated   = errors.New("unexpected end of input")
    ErrInvalidValue = errors.New("invalid value")
)
```

---

## 2. Update each error site

Use the sentinel that best describes what went wrong:

| Location | Current | Replace with |
|---|---|---|
| `parseElement` — nil peek | `ErrMalformed` | `ErrTruncated` |
| `parseElement` — unknown byte | `ErrMalformed` | `ErrUnknownType` |
| `ParseString` — missing colon | `ErrMalformed` | `ErrTruncated` |
| `ParseString` — bad length number | `ErrMalformed` | `ErrInvalidValue` |
| `ParseString` — data too short | `ErrMalformed` | `ErrTruncated` |
| `ParseInt` — missing `i` prefix | `ErrMalformed` | `ErrInvalidValue` |
| `ParseInt` — missing body | `ErrMalformed` | `ErrTruncated` |
| `ParseInt` — non-numeric body | `ErrMalformed` | `ErrInvalidValue` |
| `ParseInt` — missing `e` suffix | `ErrMalformed` | `ErrTruncated` |
| `ParseList` — missing `l` prefix | `ErrMalformed` | `ErrInvalidValue` |
| `ParseList` — missing `e` suffix | `ErrMalformed` | `ErrTruncated` |
| `ParseDict` — missing `d` prefix | `ErrMalformed` | `ErrInvalidValue` |
| `ParseDict` — bad key | `ErrMalformed` | `ErrInvalidValue` |
| `ParseDict` — bad element | `ErrMalformed` | `ErrInvalidValue` |
| `ParseDict` — missing `e` suffix | `ErrMalformed` | `ErrTruncated` |

---

## 3. Update the tests in `decode_test.go`

Each test that currently checks `errors.Is(err, ErrMalformed)` should now check the
specific sentinel. For example:

```go
// before
if !errors.Is(err, ErrMalformed) { ... }

// after — in TestParseString / "missing colon"
if !errors.Is(err, ErrTruncated) { ... }

// after — in TestParseString / "non-numeric length"
if !errors.Is(err, ErrInvalidValue) { ... }
```

Go through each test case and match it to the right sentinel from the table above.

---

## 4. Verify

```bash
go test ./internal/bencode/...
```

All tests should pass. If one fails, the sentinel you assigned to a test case does not
match what the function actually wraps — fix either the test or the error site.
