// Package abifixture contains the generated binding used by the execution ABI
// end-to-end suite.
package abifixture

// Regenerate the source-controlled Hyperion artifacts and generated binding.
// The compiler must be cyyber/hyperion@2b9a0f1d.
//
//go:generate sh -c "hypc --version 2>&1 | grep -Fq commit.2b9a0f1d || { echo 'hypc from cyyber/hyperion@2b9a0f1d is required; found:' >&2; hypc --version >&2; exit 1; }"
//go:generate hypc --abi --bin --optimize --optimize-runs 1 --no-cbor-metadata --overwrite -o testdata testdata/EventEmitter.hyp
//go:generate go run github.com/theQRL/go-qrl/cmd/abigen --abi testdata/EventEmitter.abi --bin testdata/EventEmitter.bin --pkg abifixture --type EventEmitter --out contract.go
