// Package identity holds the machine's key, certificate, and trust anchors.
//
// A machine starts unenrolled. It reads the enrollment data crux provisioned,
// generates its own Ed25519 key, and serves TLS with a certificate it signs
// itself. crux enrolls it by signing a machine certificate with the root.
// Installing that certificate saves the key, certificate, root, and
// revocation list to the state directory and deletes the enrollment data, so
// the token cannot be used again. From then on the machine loads its identity
// from the state directory, and trust only changes through newer revocation
// lists that crux sends over mutual TLS.
package identity
