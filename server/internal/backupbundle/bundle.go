// Package backupbundle implements the encrypted container format used by
// MindFS full-instance backups (".mbk" files).
//
// Layout:
//
//	line 1: JSON header + '\n'
//	  {"format":"mindfs-backup","version":1,"created_at":RFC3339,
//	   "mindfs_version":"…","kdf":{"salt_b64","iterations"},
//	   "nonce_prefix_b64":…,"chunk_size":262144,
//	   "chunks":"%010d","payload_bytes":"%016d","payload_sha256":"%064x"}
//	then: `chunks` concatenated AES-256-GCM records (ciphertext || 16-byte tag).
//
// Every data chunk decrypts to exactly chunk_size bytes of the payload except
// possibly the last data chunk (payload_bytes % chunk_size), and the final
// chunk is always a 32-byte SHA-256 marker equal to payload_sha256. Chunk
// nonces are nonce_prefix || 8-byte big-endian counter (starting at 1); the
// AAD binds the exact header line so KDF parameters, chunk counts, and the
// digest cannot be tampered with independently.
package backupbundle

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

const (
	FormatName    = "mindfs-backup"
	FormatVersion = 1

	// DefaultChunkSize is the plaintext size of every non-final chunk.
	DefaultChunkSize = 256 * 1024

	// DefaultKDFIterations is the PBKDF2-SHA256 iteration count.
	DefaultKDFIterations = 600000

	minPassphraseLen = 8

	headerMaxBytes   = 4096
	aadPrefix        = "mindfs-backup-v1|"
	chunksFieldWidth = 10 // %010d, zero padded so the header length is stable
	bytesFieldWidth  = 16 // %016d
)

var (
	ErrShortPassphrase = errors.New("passphrase must be at least 8 characters")
	ErrBadBundle       = errors.New("not a valid mindfs backup bundle")
	ErrBadPassphrase   = errors.New("wrong passphrase or corrupted bundle")
	ErrTruncated       = errors.New("bundle is truncated")
)

// Options controls Seal behavior.
type Options struct {
	Passphrase    string
	MindfsVersion string
	CreatedAt     string // RFC3339
	ChunkSize     int
	Iterations    int
}

// Seal encrypts everything from src into a new temp file inside dir ("" uses
// os.TempDir) and returns the seekable bundle file positioned at offset 0.
// The caller owns the returned file and must remove it when done.
//
// The payload is first staged to a temp file so the digest and size are known
// before the header is written — the AAD therefore binds the final header,
// and any later header edit breaks decryption.
func Seal(src io.Reader, dir string, opts Options) (*os.File, error) {
	if len([]rune(opts.Passphrase)) < minPassphraseLen {
		return nil, ErrShortPassphrase
	}
	chunkSize := opts.ChunkSize
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	iterations := opts.Iterations
	if iterations <= 0 {
		iterations = DefaultKDFIterations
	}

	// Stage the payload and compute its digest/size.
	staged, err := os.CreateTemp(dir, "mindfs-payload-*")
	if err != nil {
		return nil, fmt.Errorf("backupbundle: stage payload: %w", err)
	}
	stagedName := staged.Name()
	ok := false
	defer func() {
		staged.Close()
		if !ok {
			os.Remove(stagedName)
		}
	}()
	digest := sha256.New()
	payloadBytes, err := io.Copy(io.MultiWriter(staged, digest), src)
	if err != nil {
		return nil, fmt.Errorf("backupbundle: read payload: %w", err)
	}
	if err := staged.Sync(); err != nil {
		return nil, fmt.Errorf("backupbundle: sync payload: %w", err)
	}
	if _, err := staged.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("backupbundle: seek payload: %w", err)
	}

	salt := make([]byte, 16)
	noncePrefix := make([]byte, 4)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("backupbundle: read salt: %w", err)
	}
	if _, err := rand.Read(noncePrefix); err != nil {
		return nil, fmt.Errorf("backupbundle: read nonce prefix: %w", err)
	}

	marker := digest.Sum(nil)
	fullChunks := payloadBytes / int64(chunkSize)
	remainder := int(payloadBytes % int64(chunkSize))
	dataChunks := fullChunks
	if remainder > 0 {
		dataChunks++
	}

	hdr := newHeader(opts, chunkSize, iterations, salt, noncePrefix)
	hdr.Chunks = int(dataChunks) + 1
	hdr.PayloadBytes = payloadBytes
	hdr.PayloadSHA256 = hex.EncodeToString(marker)
	firstLine, err := hdr.encode()
	if err != nil {
		return nil, err
	}

	out, err := os.CreateTemp(dir, "mindfs-bundle-*.mbk")
	if err != nil {
		return nil, fmt.Errorf("backupbundle: create temp: %w", err)
	}
	bundleOK := false
	defer func() {
		if !bundleOK {
			out.Close()
			os.Remove(out.Name())
		}
	}()

	if _, err := out.WriteString(firstLine); err != nil {
		return nil, fmt.Errorf("backupbundle: write header: %w", err)
	}

	key := pbkdf2.Key([]byte(opts.Passphrase), salt, iterations, 32, sha256.New)
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	aad := headerAAD(firstLine)
	counter := uint64(0)

	buf := make([]byte, chunkSize)
	for i := int64(0); i < fullChunks; i++ {
		if _, err := io.ReadFull(staged, buf); err != nil {
			return nil, fmt.Errorf("backupbundle: reread payload: %w", err)
		}
		counter++
		if err := writeChunk(out, aead, noncePrefix, counter, aad, buf); err != nil {
			return nil, err
		}
	}
	if remainder > 0 {
		if _, err := io.ReadFull(staged, buf[:remainder]); err != nil {
			return nil, fmt.Errorf("backupbundle: reread payload tail: %w", err)
		}
		counter++
		if err := writeChunk(out, aead, noncePrefix, counter, aad, buf[:remainder]); err != nil {
			return nil, err
		}
	}
	counter++
	if err := writeChunk(out, aead, noncePrefix, counter, aad, marker); err != nil {
		return nil, err
	}
	if err := out.Sync(); err != nil {
		return nil, fmt.Errorf("backupbundle: sync: %w", err)
	}
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("backupbundle: seek: %w", err)
	}
	ok = true
	bundleOK = true
	return out, nil
}

// Open validates the bundle header and returns a reader over the decrypted
// payload. Wrong passphrases surface as ErrBadPassphrase when the first chunk
// is read (AEAD verification fails).
func Open(src io.Reader, passphrase string) (io.ReadCloser, error) {
	if len([]rune(passphrase)) < minPassphraseLen {
		return nil, ErrShortPassphrase
	}
	firstLine, err := readHeaderLine(src)
	if err != nil {
		return nil, err
	}
	hdr, err := parseHeader(firstLine)
	if err != nil {
		return nil, err
	}

	salt, err := base64.StdEncoding.DecodeString(hdr.KDF.SaltB64)
	if err != nil || len(salt) == 0 {
		return nil, fmt.Errorf("%w: bad kdf salt", ErrBadBundle)
	}
	noncePrefix, err := base64.StdEncoding.DecodeString(hdr.NoncePrefixB64)
	if err != nil || len(noncePrefix) != 4 {
		return nil, fmt.Errorf("%w: bad nonce prefix", ErrBadBundle)
	}

	key := pbkdf2.Key([]byte(passphrase), salt, hdr.KDF.Iterations, 32, sha256.New)
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	aad := headerAAD(string(firstLine))

	fullChunks := hdr.PayloadBytes / int64(hdr.ChunkSize)
	remainder := int(hdr.PayloadBytes % int64(hdr.ChunkSize))
	counter := uint64(0)
	digest := sha256.New()

	pr, pw := io.Pipe()
	go func() {
		fail := func(err error) { pw.CloseWithError(err) }
		read := func(size int) []byte {
			counter++
			plain, err := readChunk(src, aead, noncePrefix, counter, aad, size)
			if err != nil {
				fail(err)
				return nil
			}
			return plain
		}
		for i := int64(0); i < fullChunks; i++ {
			plain := read(int(hdr.ChunkSize))
			if plain == nil {
				return
			}
			digest.Write(plain)
			if _, err := pw.Write(plain); err != nil {
				fail(err)
				return
			}
		}
		if remainder > 0 {
			plain := read(remainder)
			if plain == nil {
				return
			}
			digest.Write(plain)
			if _, err := pw.Write(plain); err != nil {
				fail(err)
				return
			}
		}
		// Marker chunk must match both the running and recorded digests.
		marker := read(sha256.Size)
		if marker == nil {
			return
		}
		if !hmac.Equal(marker, digest.Sum(nil)) || !hmac.Equal(marker, hdr.wantDigest) {
			fail(fmt.Errorf("%w: payload digest mismatch", ErrBadPassphrase))
			return
		}
		// Nothing may trail the final chunk.
		var probe [1]byte
		if n, _ := src.Read(probe[:]); n > 0 {
			fail(fmt.Errorf("%w: trailing bytes after final chunk", ErrBadBundle))
			return
		}
		pw.Close()
	}()

	return pr, nil
}

// --- header ---

type kdfParams struct {
	SaltB64    string `json:"salt_b64"`
	Iterations int    `json:"iterations"`
}

type header struct {
	Format         string
	Version        int
	CreatedAt      string
	MindfsVersion  string
	KDF            kdfParams
	NoncePrefixB64 string
	ChunkSize      int
	Chunks         int
	PayloadBytes   int64
	PayloadSHA256  string
	wantDigest     []byte
}

func newHeader(opts Options, chunkSize, iterations int, salt, noncePrefix []byte) *header {
	return &header{
		Format:         FormatName,
		Version:        FormatVersion,
		CreatedAt:      opts.CreatedAt,
		MindfsVersion:  opts.MindfsVersion,
		KDF:            kdfParams{SaltB64: base64.StdEncoding.EncodeToString(salt), Iterations: iterations},
		NoncePrefixB64: base64.StdEncoding.EncodeToString(noncePrefix),
		ChunkSize:      chunkSize,
		Chunks:         0,
		PayloadBytes:   0,
		PayloadSHA256:  strings.Repeat("0", 64),
	}
}

func (h *header) encode() (string, error) {
	// Fixed-width numeric fields keep the encoded length stable across the
	// placeholder → final rewrite in Seal.
	payload, err := json.Marshal(struct {
		Format         string    `json:"format"`
		Version        int       `json:"version"`
		CreatedAt      string    `json:"created_at,omitempty"`
		MindfsVersion  string    `json:"mindfs_version,omitempty"`
		KDF            kdfParams `json:"kdf"`
		NoncePrefixB64 string    `json:"nonce_prefix_b64"`
		ChunkSize      int       `json:"chunk_size"`
		Chunks         string    `json:"chunks"`
		PayloadBytes   string    `json:"payload_bytes"`
		PayloadSHA256  string    `json:"payload_sha256"`
	}{
		Format:         h.Format,
		Version:        h.Version,
		CreatedAt:      h.CreatedAt,
		MindfsVersion:  h.MindfsVersion,
		KDF:            h.KDF,
		NoncePrefixB64: h.NoncePrefixB64,
		ChunkSize:      h.ChunkSize,
		Chunks:         fmt.Sprintf("%0*d", chunksFieldWidth, h.Chunks),
		PayloadBytes:   fmt.Sprintf("%0*d", bytesFieldWidth, h.PayloadBytes),
		PayloadSHA256:  h.PayloadSHA256,
	})
	if err != nil {
		return "", err
	}
	return string(payload) + "\n", nil
}

func parseHeader(line string) (*header, error) {
	var raw struct {
		Format         string    `json:"format"`
		Version        int       `json:"version"`
		CreatedAt      string    `json:"created_at"`
		MindfsVersion  string    `json:"mindfs_version"`
		KDF            kdfParams `json:"kdf"`
		NoncePrefixB64 string    `json:"nonce_prefix_b64"`
		ChunkSize      int       `json:"chunk_size"`
		Chunks         string    `json:"chunks"`
		PayloadBytes   string    `json:"payload_bytes"`
		PayloadSHA256  string    `json:"payload_sha256"`
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil, fmt.Errorf("%w: bad header json: %v", ErrBadBundle, err)
	}
	if raw.Format != FormatName || raw.Version != FormatVersion {
		return nil, fmt.Errorf("%w: unsupported format %q version %d", ErrBadBundle, raw.Format, raw.Version)
	}
	if raw.ChunkSize <= 0 || raw.ChunkSize > 64*1024*1024 {
		return nil, fmt.Errorf("%w: bad chunk size", ErrBadBundle)
	}
	chunks, err := strconv.Atoi(strings.TrimSpace(raw.Chunks))
	if err != nil || chunks < 1 {
		return nil, fmt.Errorf("%w: bad chunk count", ErrBadBundle)
	}
	payloadBytes, err := strconv.ParseInt(strings.TrimSpace(raw.PayloadBytes), 10, 64)
	if err != nil || payloadBytes < 0 {
		return nil, fmt.Errorf("%w: bad payload size", ErrBadBundle)
	}
	dataChunks := payloadBytes / int64(raw.ChunkSize)
	if payloadBytes%int64(raw.ChunkSize) != 0 {
		dataChunks++
	}
	if int64(chunks) != dataChunks+1 {
		return nil, fmt.Errorf("%w: chunk count mismatch", ErrBadBundle)
	}
	digest, err := hex.DecodeString(raw.PayloadSHA256)
	if err != nil || len(digest) != sha256.Size {
		return nil, fmt.Errorf("%w: bad payload digest", ErrBadBundle)
	}
	if raw.KDF.Iterations <= 0 || raw.KDF.Iterations > 10_000_000 {
		return nil, fmt.Errorf("%w: bad kdf iterations", ErrBadBundle)
	}
	return &header{
		Format:         raw.Format,
		Version:        raw.Version,
		CreatedAt:      raw.CreatedAt,
		MindfsVersion:  raw.MindfsVersion,
		KDF:            raw.KDF,
		NoncePrefixB64: raw.NoncePrefixB64,
		ChunkSize:      raw.ChunkSize,
		Chunks:         chunks,
		PayloadBytes:   payloadBytes,
		PayloadSHA256:  raw.PayloadSHA256,
		wantDigest:     digest,
	}, nil
}

func readHeaderLine(src io.Reader) (string, error) {
	raw := make([]byte, 0, 512)
	one := make([]byte, 1)
	for len(raw) < headerMaxBytes {
		n, err := src.Read(one)
		if n > 0 {
			if one[0] == '\n' {
				return string(raw), nil
			}
			raw = append(raw, one[0])
			continue
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("backupbundle: read header: %w", err)
		}
	}
	return "", fmt.Errorf("%w: header line missing", ErrBadBundle)
}

// --- chunk io ---

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("backupbundle: cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("backupbundle: gcm: %w", err)
	}
	return aead, nil
}

func headerAAD(firstLine string) []byte {
	sum := sha256.Sum256([]byte(strings.TrimSuffix(firstLine, "\n")))
	return []byte(aadPrefix + hex.EncodeToString(sum[:]))
}

func chunkNonce(prefix []byte, counter uint64) []byte {
	nonce := make([]byte, 0, len(prefix)+8)
	nonce = append(nonce, prefix...)
	var ctr [8]byte
	binary.BigEndian.PutUint64(ctr[:], counter)
	return append(nonce, ctr[:]...)
}

func writeChunk(out io.Writer, aead cipher.AEAD, prefix []byte, counter uint64, aad []byte, plain []byte) error {
	sealed := aead.Seal(nil, chunkNonce(prefix, counter), plain, aad)
	if _, err := out.Write(sealed); err != nil {
		return fmt.Errorf("backupbundle: write chunk: %w", err)
	}
	return nil
}

func readChunk(src io.Reader, aead cipher.AEAD, prefix []byte, counter uint64, aad []byte, plainSize int) ([]byte, error) {
	cipherBuf := make([]byte, plainSize+aead.Overhead())
	if _, err := io.ReadFull(src, cipherBuf); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil, ErrTruncated
		}
		return nil, fmt.Errorf("backupbundle: read chunk: %w", err)
	}
	plainBuf := make([]byte, plainSize)
	plain, err := aead.Open(plainBuf[:0], chunkNonce(prefix, counter), cipherBuf, aad)
	if err != nil {
		return nil, ErrBadPassphrase
	}
	return plain, nil
}

// SuggestFilename derives the conventional bundle filename from an RFC3339
// creation timestamp (e.g. 2026-09-26T17:30:00Z → mindfs-backup-20260926-173000.mbk).
func SuggestFilename(createdAt string) string {
	stamp := "backup"
	if ts, err := time.Parse(time.RFC3339, createdAt); err == nil {
		stamp = ts.Format("20060102-150405")
	}
	return "mindfs-backup-" + stamp + ".mbk"
}
