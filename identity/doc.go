// Package identity holds the agent's key, certificate, and trust anchors.
//
// The agent starts unenrolled. It reads the enrollment data crux placed on the
// machine, generates its own Ed25519 key, and serves TLS with a certificate it
// signs itself. crux enrolls it by signing an agent certificate, which names
// the machine, with the root. Installing that certificate saves the key,
// certificate, machine identifier, root, and revocation list to the state
// directory and deletes the enrollment data, so the token cannot be used
// again. From then on the agent loads its identity from the state directory,
// and trust only changes through newer revocation lists that crux sends over
// mutual TLS.
package identity
