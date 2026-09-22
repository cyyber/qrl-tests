package signing

import "crypto/sha256"

const (
	RandaoCommitmentLength = 32

	randaoOnionLayers = 1 << 20
	randaoDomainTag   = "qrl-randao-onion-v1"
)

// RandaoCommitment returns the top layer of the RANDAO hash onion seeded by an
// ML-DSA-87 signing seed: SHA-256 applied randaoOnionLayers times to the
// domain-separated origin.
func RandaoCommitment(signingSeed []byte) [RandaoCommitmentLength]byte {
	origin := sha256.New()
	origin.Write([]byte(randaoDomainTag))
	origin.Write(signingSeed)
	layer := [RandaoCommitmentLength]byte(origin.Sum(nil))

	for range randaoOnionLayers {
		layer = sha256.Sum256(layer[:])
	}
	return layer
}
