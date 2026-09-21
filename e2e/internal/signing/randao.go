package signing

import "crypto/sha256"

// RandaoOnionLayers is the hash-onion length the validator client and the
// staking deposit CLI use, so a commitment built here opens with the same
// reveals a running validator would produce.
const RandaoOnionLayers = 1 << 20

var randaoDomainTag = []byte("qrl-randao-onion-v1")

// RandaoCommitment returns the top layer of the RANDAO hash onion seeded by an
// ML-DSA-87 signing seed: SHA-256 applied RandaoOnionLayers times to the
// domain-separated origin.
func RandaoCommitment(signingSeed []byte) [RandaoCommitmentLength]byte {
	buffer := make([]byte, 0, len(randaoDomainTag)+len(signingSeed))
	buffer = append(buffer, randaoDomainTag...)
	buffer = append(buffer, signingSeed...)
	layer := sha256.Sum256(buffer)

	for range RandaoOnionLayers {
		layer = sha256.Sum256(layer[:])
	}
	return layer
}
