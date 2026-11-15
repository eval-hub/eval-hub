package oci

import (
	"errors"
	"io"
	"os"

	"github.com/eval-hub/eval-hub/pkg/api"
)

const maxSigningMaterialBytes = 1 << 20

// LocalSigningMaterial contains exact file bytes for the key decoder. It must
// never be logged, serialized into job data, or included in an error.
type LocalSigningMaterial struct {
	PrivateKey []byte
	Passphrase []byte
}

// ResolveLocalSigningMaterial reads fixed filenames confined to the selected
// directory. Call only after registry preflight, when signing starts; job
// creation validates the URI without calling this resolver.
func ResolveLocalSigningMaterial(ref string) (*LocalSigningMaterial, error) {
	directory, err := api.ParseLocalSigningDirectory(ref)
	if err != nil {
		return nil, signingMaterialError("invalid local directory URI")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, signingMaterialError("cannot open signing directory")
	}
	defer func() { _ = root.Close() }()
	key, err := readSigningFile(root, "private.key", maxSigningMaterialBytes)
	if err != nil {
		return nil, err
	}
	passphrase, err := readSigningFile(root, "passphrase.key", maxSigningMaterialBytes-len(key))
	if err != nil {
		return nil, err
	}
	return &LocalSigningMaterial{PrivateKey: key, Passphrase: passphrase}, nil
}

func readSigningFile(root *os.Root, name string, remaining int) ([]byte, error) {
	file, err := root.Open(name)
	if err != nil {
		if name == "passphrase.key" && errors.Is(err, os.ErrNotExist) {
			if _, statErr := root.Lstat(name); !errors.Is(statErr, os.ErrNotExist) {
				return nil, signingMaterialError("cannot open " + name)
			}
			// Non-nil empty bytes explicitly select an empty passphrase.
			return []byte{}, nil
		}
		return nil, signingMaterialError("cannot open " + name)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, signingMaterialError("cannot read regular file " + name)
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(remaining)+1))
	if err != nil {
		return nil, signingMaterialError("cannot read " + name)
	}
	if len(data) > remaining {
		return nil, signingMaterialError("private.key and passphrase.key exceed the combined 1 MiB limit")
	}
	return data, nil
}

func signingMaterialError(reason string) error {
	return &OperationError{Operation: "Load local signing material", Reason: reason}
}
