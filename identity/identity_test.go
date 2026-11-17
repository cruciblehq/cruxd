package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/cruciblehq/spec/trust"
	"github.com/cruciblehq/utils-go/codec"
	"github.com/cruciblehq/utils-go/file"
	"github.com/cruciblehq/utils-go/pki"
)

type testCA struct {
	root      *x509.Certificate
	rootKey   ed25519.PrivateKey
	issuer    *x509.Certificate
	issuerKey ed25519.PrivateKey
}

func sign(t *testing.T, tmpl, parent *x509.Certificate, pub ed25519.PublicKey, key ed25519.PrivateKey) *x509.Certificate {
	t.Helper()
	cert, err := pki.Sign(tmpl, parent, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func newTestCA(t *testing.T) testCA {
	t.Helper()
	rootPub, rootKey, _ := ed25519.GenerateKey(rand.Reader)
	root := sign(t, pki.AuthorityTemplate("root", pki.ValidFor(trust.RootLifetime), false), nil, rootPub, rootKey)
	issuerPub, issuerKey, _ := ed25519.GenerateKey(rand.Reader)
	issuer := sign(t, pki.AuthorityTemplate("issuer", pki.ValidFor(trust.IssuerLifetime), true), root, issuerPub, rootKey)
	return testCA{root: root, rootKey: rootKey, issuer: issuer, issuerKey: issuerKey}
}

func (ca testCA) revocations(t *testing.T, number int64) []byte {
	t.Helper()
	crl, err := pki.SignRevocationList(number, pki.ValidFor(trust.IssuerLifetime), nil, ca.root, ca.rootKey)
	if err != nil {
		t.Fatal(err)
	}
	return crl.Raw
}

func (ca testCA) clientCert(t *testing.T) tls.Certificate {
	t.Helper()
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	leaf := sign(t, pki.LeafTemplate("client", pki.ValidFor(trust.ClientLifetime), x509.ExtKeyUsageClientAuth), ca.issuer, pub, ca.issuerKey)
	return tls.Certificate{Certificate: [][]byte{leaf.Raw, ca.issuer.Raw}, PrivateKey: key}
}

func (ca testCA) signMachine(t *testing.T, csr *x509.CertificateRequest, id trust.MachineID) []byte {
	t.Helper()
	tmpl := pki.LeafTemplate(id.Name(), pki.ValidFor(trust.RootLifetime), x509.ExtKeyUsageServerAuth, id.Name())
	return sign(t, tmpl, ca.root, csr.PublicKey.(ed25519.PublicKey), ca.rootKey).Raw
}

func writeEnrollment(t *testing.T, ca testCA, token []byte) (string, string, trust.MachineID) {
	t.Helper()
	dir := t.TempDir()
	id := trust.NewMachineID()
	data, err := codec.Encode(trust.Enrollment{
		Machine:     id,
		Root:        ca.root.Raw,
		Revocations: ca.revocations(t, 1),
		Token:       token,
	}, codec.JSON)
	if err != nil {
		t.Fatal(err)
	}
	enrollment := filepath.Join(dir, "enrollment.json")
	if err := os.WriteFile(enrollment, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "state"), enrollment, id
}

func enrollSession(t *testing.T, ca testCA, id *Identity) (tls.ConnectionState, tls.ConnectionState) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var server tls.ConnectionState
	var serverErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			serverErr = err
			return
		}
		defer conn.Close()
		s := tls.Server(conn, trust.ServerConfig(id.Certificate, id.Root(), id.Revocations))
		serverErr = s.Handshake()
		server = s.ConnectionState()
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	c := tls.Client(conn, trust.EnrollConfig(ca.clientCert(t)))
	if err := c.Handshake(); err != nil {
		t.Fatal(err)
	}
	<-done
	if serverErr != nil {
		t.Fatal(serverErr)
	}
	return c.ConnectionState(), server
}

func enroll(t *testing.T, ca testCA, id *Identity, machine trust.MachineID, token []byte) []byte {
	t.Helper()
	client, server := enrollSession(t, ca, id)
	proof, err := id.Proof(server)
	if err != nil {
		t.Fatalf("Proof() = %v", err)
	}
	request, err := id.Request()
	if err != nil {
		t.Fatalf("Request() = %v", err)
	}
	csr, err := trust.VerifyEnrollment(client, request, proof, token)
	if err != nil {
		t.Fatalf("VerifyEnrollment() = %v", err)
	}
	return ca.signMachine(t, csr, machine)
}

func TestLoadNoEnrollment(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(filepath.Join(dir, "state"), filepath.Join(dir, "missing.json")); !errors.Is(err, ErrNoEnrollment) {
		t.Errorf("Load() = %v, want %v", err, ErrNoEnrollment)
	}
}

func TestLoadInvalidEnrollment(t *testing.T) {
	dir := t.TempDir()
	enrollment := filepath.Join(dir, "enrollment.json")
	if err := os.WriteFile(enrollment, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(filepath.Join(dir, "state"), enrollment); !errors.Is(err, trust.ErrInvalidEnrollment) {
		t.Errorf("Load() = %v, want %v", err, trust.ErrInvalidEnrollment)
	}
}

func TestLoadPreparesEnrollment(t *testing.T) {
	ca := newTestCA(t)
	state, enrollment, machine := writeEnrollment(t, ca, trust.NewToken())

	id, err := Load(state, enrollment)
	if err != nil {
		t.Fatalf("Load() = %v, want nil", err)
	}
	if id.Enrolled() {
		t.Error("Enrolled() = true, want false")
	}
	if !id.Root().Equal(ca.root) {
		t.Error("Root() does not match the enrollment root")
	}
	if id.Revocations().Number.Int64() != 1 {
		t.Errorf("Revocations().Number = %s, want 1", id.Revocations().Number)
	}
	leaf, err := x509.ParseCertificate(id.Certificate().Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := leaf.CheckSignature(leaf.SignatureAlgorithm, leaf.RawTBSCertificate, leaf.Signature); err != nil {
		t.Errorf("certificate is not signed by itself: %v", err)
	}
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != machine.Name() {
		t.Errorf("DNSNames = %v, want [%s]", leaf.DNSNames, machine.Name())
	}
}

func TestEnrollment(t *testing.T) {
	ca := newTestCA(t)
	token := trust.NewToken()
	state, enrollment, machine := writeEnrollment(t, ca, token)
	id, err := Load(state, enrollment)
	if err != nil {
		t.Fatal(err)
	}

	cert := enroll(t, ca, id, machine, token)
	if err := id.Install(cert); err != nil {
		t.Fatalf("Install() = %v, want nil", err)
	}

	if !id.Enrolled() {
		t.Error("Enrolled() = false, want true")
	}
	if _, err := os.Stat(enrollment); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("enrollment data still exists, stat = %v", err)
	}
	info, err := os.Stat(filepath.Join(state, "machine.key"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != file.OwnerFileMode {
		t.Errorf("key mode = %v, want %v", info.Mode().Perm(), file.OwnerFileMode)
	}
	if _, err := id.Proof(tls.ConnectionState{}); !errors.Is(err, ErrAlreadyEnrolled) {
		t.Errorf("Proof() = %v, want %v", err, ErrAlreadyEnrolled)
	}

	reloaded, err := Load(state, enrollment)
	if err != nil {
		t.Fatalf("Load() after enrollment = %v, want nil", err)
	}
	if !reloaded.Enrolled() {
		t.Error("reloaded Enrolled() = false, want true")
	}
	if string(reloaded.Certificate().Certificate[0]) != string(cert) {
		t.Error("reloaded certificate does not match the installed certificate")
	}
}

func TestInstallTwice(t *testing.T) {
	ca := newTestCA(t)
	token := trust.NewToken()
	state, enrollment, machine := writeEnrollment(t, ca, token)
	id, err := Load(state, enrollment)
	if err != nil {
		t.Fatal(err)
	}
	cert := enroll(t, ca, id, machine, token)
	if err := id.Install(cert); err != nil {
		t.Fatal(err)
	}

	if err := id.Install(cert); !errors.Is(err, ErrAlreadyEnrolled) {
		t.Errorf("Install() = %v, want %v", err, ErrAlreadyEnrolled)
	}
}

func TestInstallOtherKey(t *testing.T) {
	ca := newTestCA(t)
	state, enrollment, machine := writeEnrollment(t, ca, trust.NewToken())
	id, err := Load(state, enrollment)
	if err != nil {
		t.Fatal(err)
	}
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, other)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.ParseCertificateRequest(der)
	if err != nil {
		t.Fatal(err)
	}

	if err := id.Install(ca.signMachine(t, csr, machine)); !errors.Is(err, ErrInvalidCert) {
		t.Errorf("Install() = %v, want %v", err, ErrInvalidCert)
	}
}

func TestInstallOtherRoot(t *testing.T) {
	ca := newTestCA(t)
	token := trust.NewToken()
	state, enrollment, machine := writeEnrollment(t, ca, token)
	id, err := Load(state, enrollment)
	if err != nil {
		t.Fatal(err)
	}
	der, err := id.Request()
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.ParseCertificateRequest(der)
	if err != nil {
		t.Fatal(err)
	}

	if err := id.Install(newTestCA(t).signMachine(t, csr, machine)); !errors.Is(err, ErrInvalidCert) {
		t.Errorf("Install() = %v, want %v", err, ErrInvalidCert)
	}
}

func TestUpdateRevocations(t *testing.T) {
	ca := newTestCA(t)
	token := trust.NewToken()
	state, enrollment, machine := writeEnrollment(t, ca, token)
	id, err := Load(state, enrollment)
	if err != nil {
		t.Fatal(err)
	}
	if err := id.Install(enroll(t, ca, id, machine, token)); err != nil {
		t.Fatal(err)
	}

	if err := id.UpdateRevocations(ca.revocations(t, 3)); err != nil {
		t.Fatalf("UpdateRevocations(3) = %v, want nil", err)
	}
	if id.Revocations().Number.Int64() != 3 {
		t.Errorf("Number = %s, want 3", id.Revocations().Number)
	}
	if err := id.UpdateRevocations(ca.revocations(t, 2)); err != nil {
		t.Fatalf("UpdateRevocations(2) = %v, want nil", err)
	}
	if id.Revocations().Number.Int64() != 3 {
		t.Errorf("Number after older list = %s, want 3", id.Revocations().Number)
	}

	reloaded, err := Load(state, enrollment)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Revocations().Number.Int64() != 3 {
		t.Errorf("reloaded Number = %s, want 3", reloaded.Revocations().Number)
	}
}

func TestUpdateRevocationsOtherRoot(t *testing.T) {
	ca := newTestCA(t)
	state, enrollment, _ := writeEnrollment(t, ca, trust.NewToken())
	id, err := Load(state, enrollment)
	if err != nil {
		t.Fatal(err)
	}

	if err := id.UpdateRevocations(newTestCA(t).revocations(t, 5)); !errors.Is(err, trust.ErrInvalidRevocations) {
		t.Errorf("UpdateRevocations() = %v, want %v", err, trust.ErrInvalidRevocations)
	}
}
