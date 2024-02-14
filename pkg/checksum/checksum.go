package checksum

import (
	"crypto/sha256"
	"encoding/hex"
	"io"

	"github.com/spf13/afero"
)

func ComputeChecksum(fs afero.Fs, path string) (string, error) {
	file, err := fs.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}
