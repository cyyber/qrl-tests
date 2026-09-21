package signing

import "crypto/sha256"

const RandaoCommitmentLength = 32

// randaoOnionLayers is the hash-onion length the validator client and the
// staking deposit CLI use, so a commitment built here opens with the same
// reveals a running validator would produce.
const randaoOnionLayers = 1 << 20

const randaoDomainTag = "qrl-randao-onion-v1"

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
