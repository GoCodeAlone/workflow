//go:build !linux && !darwin

package main

import (
	"errors"
	"os"
)

func runPipelineRecord(request pipelineRecordRequest, options pipelineRecordOptions) error {
	if err := validatePipelineRecordSelectors(request.ResultStep, options.SuccessPrefix, options.ErrorPrefix); err != nil {
		return err
	}
	if err := writePipelineRecord(os.Stdout, options.ErrorPrefix, pipelineRecordErrorPayload("pipeline_failed")); err != nil {
		return errors.New("pipeline record output failed")
	}
	return errors.New("pipeline record mode is unsupported on this platform")
}

func runPipelineRecordChild() int { return 1 }
