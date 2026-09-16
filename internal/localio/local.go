// Package localio provides Gateway-shaped input and append-only JSONL output.
package localio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/AhsokaTano26/cqu-netprobe/internal/protocol"
)

type Output struct{ file *os.File }

func Open(inputPath, outputPath string) (protocol.TargetList, *Output, error) {
	input, err := os.Open(inputPath)
	if err != nil {
		return protocol.TargetList{}, nil, fmt.Errorf("open local input: %w", err)
	}
	defer input.Close()
	inputInfo, err := input.Stat()
	if err != nil {
		return protocol.TargetList{}, nil, err
	}
	targets, err := protocol.DecodeTargets(input)
	if err != nil {
		return protocol.TargetList{}, nil, err
	}
	file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return protocol.TargetList{}, nil, fmt.Errorf("open local output: %w", err)
	}
	info, err := file.Stat()
	if err == nil && os.SameFile(inputInfo, info) {
		err = errors.New("local input and output must be different files")
	}
	if err == nil && !info.Mode().IsRegular() {
		err = errors.New("local output must be a regular file")
	}
	if err == nil && info.Size() > 0 {
		var last [1]byte
		_, err = file.ReadAt(last[:], info.Size()-1)
		if err == nil && last[0] != '\n' {
			err = errors.New("existing local output must end with a newline")
		}
	}
	if err != nil {
		file.Close()
		return protocol.TargetList{}, nil, err
	}
	return targets, &Output{file: file}, nil
}

func (o *Output) Push(ctx context.Context, payload protocol.PushRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := json.NewEncoder(o.file).Encode(payload); err != nil {
		return fmt.Errorf("write local output: %w", err)
	}
	if err := o.file.Sync(); err != nil {
		return fmt.Errorf("flush local output: %w", err)
	}
	return nil
}

func (o *Output) Close() error { return o.file.Close() }
