# ABI suite

```bash
# Compile the complete suite without running live tests.
go test -tags=e2e -run '^$' ./e2e/suites/execution/abi

# Regeneration requires `hypc --version` to report commit.2b9a0f1d.
go generate ./e2e/internal/abifixture

# Run against an already-running development network.
make e2e E2E_LANE=execution-abi
```

## Coverage contract

The target is 100% of documented supported feature classes: every class has
core ABI coverage, every Go representation is generated and compiled, and each
compiler/VM/RPC boundary has a representative live assertion. It is not a
promise to exercise every type-width and nesting cross-product or to reach 100%
statement coverage.

Covered:

- Deployment: dynamic constructor strings, bytes, tuples, and arrays, verified
  through the emitted constructor event, and value accepted by the payable
  constructor.
- Calls: VM integer transition widths and extrema, booleans, 64-byte addresses,
  fixed bytes across the 32- and 64-byte boundaries, dynamic bytes and strings
  around 64-byte length boundaries, fixed and dynamic arrays, nested tuples,
  views, and overloaded methods.
- Errors: complex and zero-argument custom errors, `Error(string)`,
  `Panic(uint256)`, RPC revert data, and failed transaction receipts.
- Events and filters: successful transactions, scalar and composite events,
  supported indexed topics, indexed struct hashing, anonymous emission,
  overloaded events, and positive, negative, wildcard, and OR filters.
- Function values: two-word 68-byte values, dynamic offsets, callback
  execution, generated calls and events, and indexed hashing and filtering.
- Payable entrypoints: a named payable method plus distinct receive and
  fallback transactions.
- WebSocket subscriptions: generated watchers with matching and non-matching
  scalar and dynamically hashed indexed predicates.

Not currently supported and excluded from this target:

- Anonymous-event binding decoding and filtering: go-qrl's `bind.UnpackLog`
  rejects logs without a signature topic and abigen generates no filter,
  watch, or parse bindings for anonymous events, so their emission is
  asserted on raw logs only.
- Indexed-tuple reconstruction and filtering: topics carry only the
  Keccak-256 hash of the canonical encoding, and the generated filters type
  the tuple itself, so the hashing is asserted on raw logs only.
- Overloaded custom errors and generic `ABI.Unpack` error-name decoding.
- Anonymous tuple fields and unused fixed-point and hash ABI types.
- The upstream v2 binding API.
