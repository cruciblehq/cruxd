package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cruciblehq/spec/trust"
	"github.com/cruciblehq/utils-go/codec"
	"github.com/cruciblehq/utils-go/crex"
	"github.com/cruciblehq/utils-go/file"
	"github.com/cruciblehq/utils-go/pki"
)

// Name of the agent key in the state directory's key store.
const keyName = "machine"

// Files in the state directory.
const (
	certFile        = "machine.crt"     // DER agent certificate signed by the root.
	machineFile     = "machine.id"      // Machine identifier.
	rootFile        = "root.crt"        // DER root certificate.
	revocationsFile = "revocations.crl" // DER revocation list.
)

// Validity of the certificate the agent signs itself before enrollment.
//
// crux does not check it, because it trusts the agent through the token
// proof. It only needs to stay valid until enrollment happens.
const enrollCertLifetime = 365 * 24 * time.Hour

// Identity of the agent, backed by a state directory.
//
// Safe for concurrent use. TLS reads the certificate and revocation list on
// every handshake while handlers may install a certificate or a newer list.
type Identity struct {
	dir        string               // State directory.
	enrollment string               // Path of the enrollment data.
	machine    trust.MachineID      // Identifier crux assigned to the machine.
	key        ed25519.PrivateKey   // Agent key.
	root       *x509.Certificate    // Root that anchors all trust.
	mu         sync.RWMutex         // Guards the fields below.
	cert       *tls.Certificate     // Certificate presented in TLS.
	crl        *x509.RevocationList // Newest revocation list seen.
	token      []byte               // Enrollment token, nil once enrolled.
}

// Loads the identity from dir, or prepares enrollment from the data at enrollment.
//
// An enrolled agent loads its saved identity and deletes any enrollment data
// left behind. Otherwise the enrollment data is read and checked, and a new
// key is generated. An empty enrollment means there is no enrollment data.
// Returns [ErrNoEnrollment] when the agent is not enrolled and has no
// enrollment data.
func Load(dir, enrollment string) (*Identity, error) {
	if _, err := os.Stat(filepath.Join(dir, certFile)); err == nil {
		id, err := load(dir)
		if err != nil {
			return nil, err
		}
		if err := removeEnrollment(enrollment); err != nil {
			return nil, err
		}
		return id, nil
	}
	if enrollment == "" {
		return nil, crex.Newf(ErrNoEnrollment, "no enrollment path is set")
	}
	return prepare(dir, enrollment)
}

// Deletes the enrollment data at path, if any.
func removeEnrollment(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Loads a saved identity from dir.
func load(dir string) (*Identity, error) {
	key, err := loadKey(dir)
	if err != nil {
		return nil, err
	}
	id, err := loadMachineID(dir)
	if err != nil {
		return nil, err
	}
	root, err := loadCertificate(dir, rootFile)
	if err != nil {
		return nil, err
	}
	crl, err := loadRevocations(dir, root)
	if err != nil {
		return nil, err
	}
	cert, err := loadCertificate(dir, certFile)
	if err != nil {
		return nil, err
	}

	return &Identity{
		dir:     dir,
		machine: id,
		key:     key,
		root:    root,
		cert:    &tls.Certificate{Certificate: [][]byte{cert.Raw}, PrivateKey: key, Leaf: cert},
		crl:     crl,
	}, nil
}

// Loads the agent key from the key store in dir.
func loadKey(dir string) (ed25519.PrivateKey, error) {
	signer, err := pki.NewDirKeyStore(dir).Load(keyName)
	if err != nil {
		return nil, crex.Wrap(ErrInvalidState, err)
	}
	key, ok := signer.(ed25519.PrivateKey)
	if !ok {
		return nil, crex.Newf(ErrInvalidState, "unexpected key type %T", signer)
	}
	return key, nil
}

// Reads and validates the machine identifier saved in dir.
func loadMachineID(dir string) (trust.MachineID, error) {
	data, err := os.ReadFile(filepath.Join(dir, machineFile))
	if err != nil {
		return "", crex.Wrap(ErrInvalidState, err)
	}
	id := trust.MachineID(data)
	if err := id.Validate(); err != nil {
		return "", err
	}
	return id, nil
}

// Reads and parses the DER certificate in the file name in dir.
func loadCertificate(dir, name string) (*x509.Certificate, error) {
	der, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil, crex.Wrap(ErrInvalidState, err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, crex.Wrap(ErrInvalidState, err)
	}
	return cert, nil
}

// Reads the revocation list saved in dir and checks that root signed it.
func loadRevocations(dir string, root *x509.Certificate) (*x509.RevocationList, error) {
	der, err := os.ReadFile(filepath.Join(dir, revocationsFile))
	if err != nil {
		return nil, crex.Wrap(ErrInvalidState, err)
	}
	return trust.ParseRevocations(der, root)
}

// Reads the enrollment data, generates a new key, and signs a certificate
// for it with that key.
func prepare(dir, enrollment string) (*Identity, error) {
	data, err := os.ReadFile(enrollment)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, crex.Newf(ErrNoEnrollment, "%q does not exist", enrollment)
	}
	if err != nil {
		return nil, err
	}
	var e trust.Enrollment
	if err := codec.Decode(data, &e, codec.JSON); err != nil {
		return nil, crex.Wrap(trust.ErrInvalidEnrollment, err)
	}
	root, crl, err := e.Verify()
	if err != nil {
		return nil, err
	}

	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	tmpl := pki.LeafTemplate(e.Machine.Name(), pki.ValidFor(enrollCertLifetime), x509.ExtKeyUsageServerAuth, e.Machine.Name())
	cert, err := pki.Sign(tmpl, nil, pub, key)
	if err != nil {
		return nil, err
	}

	return &Identity{
		dir:        dir,
		enrollment: enrollment,
		machine:    e.Machine,
		key:        key,
		root:       root,
		cert:       &tls.Certificate{Certificate: [][]byte{cert.Raw}, PrivateKey: key, Leaf: cert},
		crl:        crl,
		token:      e.Token,
	}, nil
}

// Whether the agent has installed a certificate signed by the root.
func (i *Identity) Enrolled() bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.token == nil
}

// Returns the certificate the agent presents in TLS.
func (i *Identity) Certificate() *tls.Certificate {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.cert
}

// Returns the root certificate.
func (i *Identity) Root() *x509.Certificate {
	return i.root
}

// Returns the newest revocation list the agent has seen.
func (i *Identity) Revocations() *x509.RevocationList {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.crl
}

// Returns the token proof for the TLS session in state.
//
// Returns [ErrAlreadyEnrolled] once the token has been used.
func (i *Identity) Proof(state tls.ConnectionState) (trust.Proof, error) {
	i.mu.RLock()
	token := i.token
	i.mu.RUnlock()
	if token == nil {
		return trust.Proof{}, crex.Newf(ErrAlreadyEnrolled, "the agent on machine %q has no enrollment token", i.machine)
	}
	ekm, err := trust.Exporter(state)
	if err != nil {
		return trust.Proof{}, err
	}
	return trust.TokenProof(token, ekm), nil
}

// Returns a DER certificate request for the agent key and the machine name.
func (i *Identity) Request() ([]byte, error) {
	return x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: i.machine.Name()},
		DNSNames: []string{i.machine.Name()},
	}, i.key)
}

// Installs the agent certificate crux signed and completes enrollment.
//
// The certificate must hold the agent key and pass the same checks crux
// applies when it connects. The identity is saved before the enrollment data
// is deleted, so a crash in between leaves the agent enrolled. Returns
// [ErrAlreadyEnrolled] when a certificate is already installed.
func (i *Identity) Install(der []byte) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.token == nil {
		return crex.Newf(ErrAlreadyEnrolled, "the agent on machine %q already has a certificate", i.machine)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return crex.Wrap(ErrInvalidCert, err)
	}
	if !i.key.Public().(ed25519.PublicKey).Equal(cert.PublicKey) {
		return crex.Newf(ErrInvalidCert, "certificate key does not match the agent key on machine %q", i.machine)
	}
	roots := x509.NewCertPool()
	roots.AddCert(i.root)
	chains, err := cert.Verify(x509.VerifyOptions{
		Roots:     roots,
		DNSName:   i.machine.Name(),
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	if err != nil {
		return crex.Wrap(ErrInvalidCert, err)
	}
	if err := trust.VerifyServer(chains, i.crl); err != nil {
		return err
	}

	if err := i.save(cert); err != nil {
		return err
	}
	if err := removeEnrollment(i.enrollment); err != nil {
		return err
	}
	i.cert = &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: i.key, Leaf: cert}
	i.token = nil
	return nil
}

// Stores der as the revocation list if the root signed it and it is newer.
//
// An older or equal list is ignored. Before enrollment the list is kept only
// in memory, and enrollment saves it. Returns an error when the list is not
// valid.
func (i *Identity) UpdateRevocations(der []byte) error {
	crl, err := trust.ParseRevocations(der, i.root)
	if err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if !trust.Newer(crl, i.crl) {
		return nil
	}
	if i.token == nil {
		if err := file.WriteFile(filepath.Join(i.dir, revocationsFile), crl.Raw, file.OwnerFileMode); err != nil {
			return err
		}
	}
	i.crl = crl
	return nil
}

// Saves the key, certificate, machine identifier, root, and revocation list.
//
// The certificate is written last, because its presence marks the agent as
// enrolled.
func (i *Identity) save(cert *x509.Certificate) error {
	if err := os.MkdirAll(i.dir, file.OwnerDirMode); err != nil {
		return err
	}
	if err := pki.NewDirKeyStore(i.dir).Store(keyName, i.key); err != nil {
		return err
	}
	files := []struct {
		name string
		data []byte
	}{
		{machineFile, []byte(i.machine)},
		{rootFile, i.root.Raw},
		{revocationsFile, i.crl.Raw},
		{certFile, cert.Raw},
	}
	for _, f := range files {
		if err := file.WriteFile(filepath.Join(i.dir, f.name), f.data, file.OwnerFileMode); err != nil {
			return err
		}
	}
	return nil
}
