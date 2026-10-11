package backupbundle

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func sealBytes(t *testing.T, payload []byte, passphrase string, chunkSize int) (*os.File, error) {
	t.Helper()
	return Seal(bytes.NewReader(payload), t.TempDir(), Options{
		Passphrase: passphrase,
		ChunkSize:  chunkSize,
		CreatedAt:  "2026-09-26T17:30:00Z",
	})
}

func openBytes(t *testing.T, file *os.File, passphrase string) ([]byte, error) {
	t.Helper()
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	rc, err := Open(file, passphrase)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func TestRoundTrip(t *testing.T) {
	cases := []struct {
		name      string
		size      int
		chunkSize int
	}{
		{"empty", 0, 1024},
		{"partial chunk", 100, 1024},
		{"exact chunk", 1024, 1024},
		{"exact plus partial", 2048 + 5, 1024},
		{"large multi", 300*1024 + 17, 64 * 1024},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := make([]byte, tc.size)
			for i := range payload {
				payload[i] = byte(i % 251)
			}
			file, err := sealBytes(t, payload, "correct horse battery", tc.chunkSize)
			if err != nil {
				t.Fatalf("seal: %v", err)
			}
			defer os.Remove(file.Name())
			got, err := openBytes(t, file, "correct horse battery")
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("payload mismatch: got %d bytes", len(got))
			}
		})
	}
}

func TestWrongPassphrase(t *testing.T) {
	file, err := sealBytes(t, []byte("secret payload data"), "passphrase-1", 64)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	defer os.Remove(file.Name())
	if _, err := openBytes(t, file, "passphrase-2"); !errors.Is(err, ErrBadPassphrase) {
		t.Fatalf("err = %v, want ErrBadPassphrase", err)
	}
}

func TestShortPassphraseRejected(t *testing.T) {
	if _, err := Seal(strings.NewReader("x"), t.TempDir(), Options{Passphrase: "short"}); !errors.Is(err, ErrShortPassphrase) {
		t.Fatalf("err = %v, want ErrShortPassphrase", err)
	}
	if _, err := Open(strings.NewReader("x"), "short"); !errors.Is(err, ErrShortPassphrase) {
		t.Fatalf("err = %v, want ErrShortPassphrase", err)
	}
}

func TestTamperedHeader(t *testing.T) {
	file, err := sealBytes(t, []byte("secret payload data"), "passphrase-1", 64)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	defer os.Remove(file.Name())
	raw, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	idx := bytes.Index(raw, []byte(`"chunk_size":64`))
	if idx < 0 {
		t.Fatalf("chunk_size field not found in header: %s", raw[:80])
	}
	raw[idx+len(`"chunk_size":`)] = '3' // 64 → 34
	if err := os.WriteFile(file.Name(), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openBytes(t, file, "passphrase-1"); err == nil {
		t.Fatal("expected error for tampered header")
	}
}

func TestFlippedCiphertext(t *testing.T) {
	file, err := sealBytes(t, bytes.Repeat([]byte("a"), 200), "passphrase-1", 64)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	defer os.Remove(file.Name())
	raw, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-20] ^= 0xFF
	if err := os.WriteFile(file.Name(), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openBytes(t, file, "passphrase-1"); err == nil {
		t.Fatal("expected error for flipped ciphertext")
	}
}

func TestTruncatedStream(t *testing.T) {
	file, err := sealBytes(t, bytes.Repeat([]byte("a"), 300), "passphrase-1", 64)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	defer os.Remove(file.Name())
	raw, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file.Name(), raw[:len(raw)-30], 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openBytes(t, file, "passphrase-1"); err == nil {
		t.Fatal("expected error for truncated bundle")
	}
}

func TestTrailingGarbage(t *testing.T) {
	file, err := sealBytes(t, []byte("payload"), "passphrase-1", 64)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	defer os.Remove(file.Name())
	f, err := os.OpenFile(file.Name(), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("garbage")); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := openBytes(t, file, "passphrase-1"); err == nil {
		t.Fatal("expected error for trailing bytes")
	}
}

func TestNotABundle(t *testing.T) {
	if _, err := Open(strings.NewReader("hello world\nrest"), "passphrase-1"); err == nil {
		t.Fatal("expected ErrBadBundle")
	}
}

func TestSuggestFilename(t *testing.T) {
	got := SuggestFilename("2026-09-26T17:30:00Z")
	if got != "mindfs-backup-20260926-173000.mbk" {
		t.Fatalf("got %q", got)
	}
	if SuggestFilename("junk") != "mindfs-backup-backup.mbk" {
		t.Fatalf("fallback name unexpected")
	}
}
