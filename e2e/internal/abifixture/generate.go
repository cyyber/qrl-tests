// Package abifixture contains the generated binding used by the execution ABI
// end-to-end suite.
package abifixture

// Regenerate the Hyperion artifacts and the Go binding; the ABI is
// source-controlled, the bytecode stays ephemeral and embedded.
// The compiler must be cyyber/hyperion@2b9a0f1d.
//
// abigen consumes the combined JSON so it can wire the Math512 library
// deployment and link its placeholder inside EventEmitter's bytecode.
//
//go:generate sh -c "hypc --version 2>&1 | grep -Fq commit.2b9a0f1d || { echo 'hypc from cyyber/hyperion@2b9a0f1d is required; found:' >&2; hypc --version >&2; exit 1; }"
//go:generate hypc --abi --optimize --optimize-runs 1 --no-cbor-metadata --overwrite -o testdata testdata/EventEmitter.hyp
//go:generate sh -c "hypc --combined-json abi,bin --optimize --optimize-runs 1 --no-cbor-metadata testdata/EventEmitter.hyp > testdata/combined.json"
//go:generate go tool abigen --combined-json testdata/combined.json --pkg abifixture --out contract.go
