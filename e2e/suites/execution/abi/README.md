# ABI suite

Compile the complete suite without running live tests:

```bash
go test -tags=e2e -run '^$' ./e2e/suites/execution/abi
```

Regenerate the bindings after changing the fixture contract at
[`contracts/testdata/EventEmitter.hyp`](contracts/testdata/EventEmitter.hyp) —
`hypc --version` must report `commit.2b9a0f1d`:

```bash
go generate ./e2e/suites/execution/abi/contracts
```

Run against an already-running development network:

```bash
make e2e E2E_LANE=execution E2E_SUITE=execution-abi
```

## Coverage contract

Every supported feature class gets generated-and-compiled Go bindings and a
representative live assertion across the compiler, VM, and RPC boundaries. The
target is class coverage — not every type-width and nesting cross-product, and
not statement coverage.

Covered:

- Deployment: dynamic constructor strings, bytes, tuples, and arrays, verified
  through the emitted constructor event, and value accepted by the payable
  constructor.
- Calls: VM integer transition widths and edge values, booleans, 64-byte addresses,
  fixed bytes across the 32- and 64-byte boundaries, dynamic bytes and strings
  around 64-byte length boundaries, fixed and dynamic arrays, nested tuples,
  views, and overloaded methods.
- Library linking: an external library deployed and linked by the generated
  bindings, with its delegatecalled function exercised live.
- Errors: complex and zero-argument custom errors, `Error(string)`,
  `Panic(uint256)`, RPC revert data, and failed transaction receipts.
- Events and filters: scalar and composite events, supported indexed topics,
  indexed struct hashing, anonymous emission, overloaded events, and positive,
  negative, wildcard, and OR filters.
- Function values: standalone 68-byte values, dynamic offsets, callback
  execution, generated calls and events, and indexed hashing and filtering.
- Payable entrypoints: a named payable method plus distinct receive and
  fallback transactions.
- WebSocket subscriptions: generated watchers with matching and non-matching
  scalar and dynamically hashed indexed predicates.

Excluded — not currently supported:

- Anonymous-event binding decoding and filtering: go-qrl's `bind.UnpackLog`
  rejects logs without a signature topic and abigen generates no filter,
  watch, or parse bindings for anonymous events, so their emission is
  asserted on raw logs only.
- Indexed-tuple reconstruction and filtering: topics carry only the
  Keccak-256 hash of the canonical encoding, and the generated filters type
  the tuple itself, so the hashing is asserted on raw logs only.
- Overloaded custom errors and generic `ABI.Unpack` error-name decoding.
- Anonymous tuple fields and unused fixed-point and hash ABI types.
