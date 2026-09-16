package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/AhsokaTano26/cqu-netprobe/internal/limits"
)

// DecodeTargets is shared by Gateway responses and local input files.
func DecodeTargets(reader io.Reader) (TargetList, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limits.MaxTargetConfigBytes+1))
	if err != nil {
		return TargetList{}, fmt.Errorf("read targets: %w", err)
	}
	if len(body) > limits.MaxTargetConfigBytes {
		return TargetList{}, fmt.Errorf("target configuration exceeds %d MiB", limits.MaxTargetConfigBytes>>20)
	}
	var targets TargetList
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&targets); err != nil {
		return TargetList{}, fmt.Errorf("decode targets: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return TargetList{}, errors.New("target configuration contains trailing or invalid JSON data")
	}
	if err := targets.Validate(); err != nil {
		return TargetList{}, fmt.Errorf("validate targets: %w", err)
	}
	return targets, nil
}
