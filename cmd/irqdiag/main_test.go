package main

import (
	"bytes"
	"context"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunVersion(t *testing.T) {
	var output bytes.Buffer
	require.NoError(t, run(context.Background(), []string{"--version"}, &output))
	require.Contains(t, output.String(), Version)
}

func TestRunRejectsNegativeSampleDuration(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("platform error precedes duration validation outside Linux")
	}
	var output bytes.Buffer
	require.Error(t, run(context.Background(), []string{"--sample-duration=-1s"}, &output))
}
