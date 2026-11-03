package devnet

import (
	"fmt"
	"slices"
	"strings"
)

type Profile string

var (
	// defaultCLExtraParams let a beacon node without peers count as synced.
	defaultCLExtraParams = []string{"--min-sync-peers=0", "--minimum-peers-per-subnet=0"}
	anchorCLExtraParams  = append(slices.Clone(defaultCLExtraParams), "--sync-from=head")
)

// Further profiles arrive together with the lanes that exercise them.
const (
	ProfileSingle Profile = "single"
	// ProfileAnchored has two participants that share the validators. The
	// first is the anchor of a full-outage recovery: its beacon node counts
	// as synced without peers and starts from its stored head. By default a
	// beacon node starts from the justified checkpoint, and a network whose
	// beacon nodes all stopped never proposes again. The second keeps the
	// package's sync settings, so it follows the anchor.
	ProfileAnchored Profile = "anchored"
	// ProfileRemoteSigner has one participant whose validator client holds no
	// keys: go-remotesigner signs every duty with the deposit CLI keystores
	// qrl-package generated for it. Clef, the execution signer, does not run,
	// since qrl-package gives one participant a single remote signer.
	ProfileRemoteSigner Profile = "remote-signer"
	// ProfileRemoteSignerMTLS is ProfileRemoteSigner with go-remotesigner in
	// token+mtls mode: the validator client presents a client certificate
	// as well as the sign token. It needs a qrl-package with the participant
	// field remote_signer_auth_mode, which no qrl-package has yet.
	ProfileRemoteSignerMTLS Profile = "remote-signer-mtls"
	// ProfileRemoteSignerExits is ProfileRemoteSigner with go-remotesigner
	// signing voluntary exits, which it refuses unless it runs with
	// --voluntary-exit-signing-enabled.
	ProfileRemoteSignerExits Profile = "remote-signer-exits"
	// ProfileRemoteSignerExecution has one participant whose execution
	// client's JSON-RPC go-remotesigner fronts in execution mode: it holds
	// qrl-package's development accounts, signs and sends their transactions
	// through the client, and forwards to it what its allowlist allows. Clef
	// does not run, as in ProfileRemoteSigner, and the validator client keeps
	// its keystores.
	ProfileRemoteSignerExecution Profile = "remote-signer-execution"
)

// remoteSignerExitsFlag is the go-remotesigner consensus flag that has it
// sign voluntary exits.
const remoteSignerExitsFlag = "--voluntary-exit-signing-enabled"

// go-remotesigner's API authentication, its --http-auth-mode, in the profiles
// that run it.
const (
	RemoteSignerAuthToken     = "token"
	RemoteSignerAuthTokenMTLS = "token+mtls"
)

type profileSpec struct {
	participants            []participantSpec
	preregisteredValidators int
}

type participantSpec struct {
	validatorCount int
	elExtraParams  []string
	clExtraParams  []string
	vcExtraParams  []string
	remoteSigner   bool
	// remoteSignerAuthMode is the signer's authentication when it is not
	// qrl-package's default, token.
	remoteSignerAuthMode string
	// remoteSignerExtraParams are go-remotesigner consensus flags that
	// qrl-package passes after its own.
	remoteSignerExtraParams []string
	// keymanager has the validator client serve the keymanager API.
	keymanager bool
	// executionSigner runs go-remotesigner in execution mode in front of the
	// execution client, in place of clef.
	executionSigner bool
}

var profileSpecs = map[Profile]profileSpec{
	ProfileSingle: {participants: []participantSpec{{validatorCount: 64}}},
	ProfileAnchored: {participants: []participantSpec{
		{validatorCount: 32, clExtraParams: anchorCLExtraParams},
		{validatorCount: 32, clExtraParams: []string{"--minimum-peers-per-subnet=0"}},
	}},
	// The remote-signer profiles' validator clients serve the keymanager
	// API, whose remote keys endpoints change the keys they sign for.
	ProfileRemoteSigner: {participants: []participantSpec{{validatorCount: 64, remoteSigner: true, keymanager: true}}},
	ProfileRemoteSignerMTLS: {participants: []participantSpec{
		{validatorCount: 64, remoteSigner: true, keymanager: true, remoteSignerAuthMode: RemoteSignerAuthTokenMTLS},
	}},
	ProfileRemoteSignerExits: {participants: []participantSpec{
		{validatorCount: 64, remoteSigner: true, keymanager: true, remoteSignerExtraParams: []string{remoteSignerExitsFlag}},
	}},
	ProfileRemoteSignerExecution: {participants: []participantSpec{{validatorCount: 64, executionSigner: true}}},
}

// ParseProfile validates the raw value, resolving the empty value to the
// default single-participant profile; only a verified value becomes a Profile.
func ParseProfile(value string) (Profile, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ProfileSingle, nil
	}

	profile := Profile(trimmed)
	if _, exists := profileSpecs[profile]; !exists {
		return "", fmt.Errorf("unknown development-network profile %q", value)
	}
	return profile, nil
}

// RemoteSignerAuthMode returns how the validator clients of the profile
// authenticate to go-remotesigner: RemoteSignerAuthToken or
// RemoteSignerAuthTokenMTLS, or "" when no participant signs through it.
func (profile Profile) RemoteSignerAuthMode() string {
	for _, participant := range profileSpecs[profile].participants {
		if !participant.remoteSigner {
			continue
		}
		if participant.remoteSignerAuthMode != "" {
			return participant.remoteSignerAuthMode
		}
		return RemoteSignerAuthToken
	}
	return ""
}

// RemoteSignerSignsExits reports whether the go-remotesigner of the profile
// signs voluntary exits; by default it refuses them.
func (profile Profile) RemoteSignerSignsExits() bool {
	for _, participant := range profileSpecs[profile].participants {
		if participant.remoteSigner && slices.Contains(participant.remoteSignerExtraParams, remoteSignerExitsFlag) {
			return true
		}
	}
	return false
}

// RunsExecutionSigner reports whether a participant of the profile runs
// go-remotesigner in execution mode in front of its execution client.
func (profile Profile) RunsExecutionSigner() bool {
	return slices.ContainsFunc(profileSpecs[profile].participants, func(participant participantSpec) bool {
		return participant.executionSigner
	})
}
