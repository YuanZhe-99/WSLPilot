package wpbk

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/zeebo/blake3"
	"golang.org/x/crypto/argon2"
)

// Encryption identifiers and parameters (see backup-format.md, "Encryption").
const (
	// EncryptionAESGCM is the only encryption algorithm v1 defines.
	EncryptionAESGCM = "aes-256-gcm"
	// KDFArgon2id is the only password key derivation function v1 defines.
	KDFArgon2id = "argon2id"
)

// Argon2id parameters writers use for new repositories and new passwords.
const (
	kdfTime    = 3
	kdfMemory  = 64 * 1024 // KiB
	kdfThreads = 4
	kdfSaltLen = 16
)

// Limits a reader accepts for KDF parameters found in a config, so a
// hostile config.json cannot make Open allocate unbounded memory.
const (
	maxKDFTime   = 64
	maxKDFMemory = 4 << 20 // KiB, 4 GiB
)

// Sizes of the sealed format.
const (
	keyLen   = 32
	nonceLen = 12
	tagLen   = 16
)

// HKDF info strings that derive the subkeys from the master key.
const (
	infoDataKey = "wpbk v1 data key"
	infoIDKey   = "wpbk v1 chunk id key"
)

// Additional authenticated data for sealed documents; blobs use the 32
// raw bytes of their chunk id instead.
var (
	aadTrailer  = []byte("wpbk trailer")
	aadIndex    = []byte("wpbk index")
	aadSnapshot = []byte("wpbk snapshot")
)

// Encryption is config.json's "encryption" object of an encrypted
// repository.
type Encryption struct {
	Algorithm string    `json:"algorithm"`
	KDF       KDFParams `json:"kdf"`
	// Key is base64(nonce || AES-256-GCM(KEK, master key)).
	Key string `json:"key"`
}

// KDFParams are the Argon2id parameters that turn the password into the
// key-encryption key (KEK).
type KDFParams struct {
	Name string `json:"name"`
	// Salt is standard base64 with padding.
	Salt    string `json:"salt"`
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"` // KiB
	Threads uint8  `json:"threads"`
}

// cryptoState holds the derived keys of an open encrypted repository.
type cryptoState struct {
	aead  cipher.AEAD
	idKey []byte
}

// parseEncryption decodes a non-null "encryption" value and validates it.
// Returns: the parameters, or ErrUnsupportedVersion (wrapped) when the
// algorithm or KDF is unknown or the parameters are out of range.
func parseEncryption(raw json.RawMessage) (*Encryption, error) {
	var e Encryption
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, fmt.Errorf("%w: encryption: %v", ErrNotRepository, err)
	}
	if e.Algorithm != EncryptionAESGCM {
		return nil, fmt.Errorf("%w: encryption algorithm %q", ErrUnsupportedVersion, e.Algorithm)
	}
	if e.KDF.Name != KDFArgon2id {
		return nil, fmt.Errorf("%w: key derivation %q", ErrUnsupportedVersion, e.KDF.Name)
	}
	salt, err := base64.StdEncoding.DecodeString(e.KDF.Salt)
	if err != nil || len(salt) < 8 {
		return nil, fmt.Errorf("%w: bad key derivation salt", ErrNotRepository)
	}
	if e.KDF.Time < 1 || e.KDF.Time > maxKDFTime || e.KDF.Memory < 8*uint32(max(e.KDF.Threads, 1)) || e.KDF.Memory > maxKDFMemory || e.KDF.Threads < 1 {
		return nil, fmt.Errorf("%w: key derivation parameters out of range", ErrUnsupportedVersion)
	}
	if k, err := base64.StdEncoding.DecodeString(e.Key); err != nil || len(k) != nonceLen+keyLen+tagLen {
		return nil, fmt.Errorf("%w: bad wrapped key", ErrNotRepository)
	}
	return &e, nil
}

// kek derives the key-encryption key from password with e's parameters.
func (e *Encryption) kek(password string) []byte {
	salt, _ := base64.StdEncoding.DecodeString(e.KDF.Salt)
	return argon2.IDKey([]byte(password), salt, e.KDF.Time, e.KDF.Memory, e.KDF.Threads, keyLen)
}

// unwrap recovers the master key with password.
// Returns: the master key, or ErrPassword when the password is wrong.
func (e *Encryption) unwrap(password string) ([]byte, error) {
	wrapped, _ := base64.StdEncoding.DecodeString(e.Key)
	aead := newGCM(e.kek(password))
	master, err := aead.Open(nil, wrapped[:nonceLen], wrapped[nonceLen:], nil)
	if err != nil {
		return nil, ErrPassword
	}
	return master, nil
}

// newEncryption wraps master under password with fresh salt and the
// default Argon2id parameters.
func newEncryption(master []byte, password string) *Encryption {
	salt := make([]byte, kdfSaltLen)
	rand.Read(salt)
	e := &Encryption{
		Algorithm: EncryptionAESGCM,
		KDF:       KDFParams{Name: KDFArgon2id, Salt: base64.StdEncoding.EncodeToString(salt), Time: kdfTime, Memory: kdfMemory, Threads: kdfThreads},
	}
	aead := newGCM(e.kek(password))
	nonce := make([]byte, nonceLen)
	rand.Read(nonce)
	e.Key = base64.StdEncoding.EncodeToString(aead.Seal(nonce, nonce, master, nil))
	return e
}

// newCryptoState derives the data and chunk-id subkeys from master.
func newCryptoState(master []byte) (*cryptoState, error) {
	dk, err := hkdf.Key(sha256.New, master, nil, infoDataKey, keyLen)
	if err != nil {
		return nil, err
	}
	ik, err := hkdf.Key(sha256.New, master, nil, infoIDKey, keyLen)
	if err != nil {
		return nil, err
	}
	return &cryptoState{aead: newGCM(dk), idKey: ik}, nil
}

// newGCM returns AES-256-GCM with a 12-byte nonce for a 32-byte key.
func newGCM(key []byte) cipher.AEAD {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err) // key length is always 32
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return aead
}

// Encrypted reports whether the repository encrypts its data.
func (r *Repo) Encrypted() bool { return r.crypt != nil }

// chunkID returns the id of a chunk with plaintext b: plain BLAKE3-256 in
// an unencrypted repository, BLAKE3 keyed with the chunk-id subkey in an
// encrypted one.
func (r *Repo) chunkID(b []byte) string {
	if r.crypt == nil {
		return hashID(b)
	}
	h, err := blake3.NewKeyed(r.crypt.idKey)
	if err != nil {
		panic(err) // key length is always 32
	}
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// seal encrypts data with the data subkey when the repository is
// encrypted, and returns it unchanged otherwise.
// Returns: nonce(12) || AES-256-GCM ciphertext and tag.
func (r *Repo) seal(aad, data []byte) []byte {
	if r.crypt == nil {
		return data
	}
	out := make([]byte, nonceLen, nonceLen+len(data)+tagLen)
	if _, err := rand.Read(out); err != nil {
		panic(err)
	}
	return r.crypt.aead.Seal(out, out, data, aad)
}

// unseal reverses seal.
// Returns: the plaintext, or an error when the bytes fail authentication.
func (r *Repo) unseal(aad, data []byte) ([]byte, error) {
	if r.crypt == nil {
		return data, nil
	}
	if len(data) < nonceLen+tagLen {
		return nil, fmt.Errorf("sealed data too short")
	}
	plain, err := r.crypt.aead.Open(nil, data[:nonceLen], data[nonceLen:], aad)
	if err != nil {
		return nil, fmt.Errorf("authentication failed")
	}
	return plain, nil
}

// blobAAD returns the additional data of a blob: the raw 32-byte chunk id.
func blobAAD(id string) []byte {
	b, _ := hex.DecodeString(id)
	return b
}
