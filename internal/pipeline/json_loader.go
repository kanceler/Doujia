package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

func LoadRegistrySpec(path string) (RegistrySpec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return RegistrySpec{}, err
	}
	spec, err := ParseRegistrySpec(raw)
	if err != nil {
		return RegistrySpec{}, fmt.Errorf("load pipeline registry %s: %w", path, err)
	}
	return spec, nil
}

func ParseRegistrySpec(raw []byte) (RegistrySpec, error) {
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if bytes.Contains(raw, []byte(`"bag_role"`)) {
		return RegistrySpec{}, fmt.Errorf("bag_role is not supported; name is required")
	}
	var spec RegistrySpec
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&spec); err != nil {
		return RegistrySpec{}, err
	}
	spec = NormalizeRegistrySpec(spec)
	if err := ValidateRegistrySpec(spec); err != nil {
		return RegistrySpec{}, err
	}
	return spec, nil
}

func DecodeRegistrySpec(reader io.Reader) (RegistrySpec, error) {
	raw, err := io.ReadAll(reader)
	if err != nil {
		return RegistrySpec{}, err
	}
	return ParseRegistrySpec(raw)
}
