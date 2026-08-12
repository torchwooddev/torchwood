package functions

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadBuildOutput_SuccessStream(t *testing.T) {
	stream := "{\"stream\":\"Step 1/2 : FROM node:18-alpine\\n\"}\n" +
		"{\"stream\":\"Successfully built abc123\\n\"}\n"
	log, err := readBuildOutput(strings.NewReader(stream))
	require.NoError(t, err)
	require.Contains(t, log, "Step 1/2")
	require.Contains(t, log, "Successfully built")
}

func TestReadBuildOutput_ErrorJSON(t *testing.T) {
	stream := "{\"stream\":\"Step 2/2 : RUN nope\\n\"}\n" +
		"{\"errorDetail\":{\"message\":\"The command '/bin/sh -c nope' returned a non-zero code: 127\"},\"error\":\"The command '/bin/sh -c nope' returned a non-zero code: 127\"}\n"
	log, err := readBuildOutput(strings.NewReader(stream))
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-zero code")
	require.Contains(t, log, "The command '/bin/sh -c nope'")
}

func TestReadBuildOutput_ErrorDetailOnly(t *testing.T) {
	stream := "{\"errorDetail\":{\"message\":\"failed to solve: no matching manifest\"}}\n"
	_, err := readBuildOutput(strings.NewReader(stream))
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to solve")
}

func TestReadBuildOutput_LongStreamKeepsErrorTail(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < (maxBuildLogBytes/16)+100; i++ {
		sb.WriteString("{\"stream\":\"some build progress line content\\n\"}\n")
	}
	sb.WriteString("{\"error\":\"the final failure\"}\n")
	log, err := readBuildOutput(strings.NewReader(sb.String()))
	require.Error(t, err)
	require.Contains(t, err.Error(), "the final failure")
	require.Len(t, log, maxBuildLogBytes, "日志保留尾部 64KB")
	require.True(t, strings.HasSuffix(log, "{\"error\":\"the final failure\"}\n"), "错误行位于日志末尾")
}

func TestReadBuildOutput_PlainTextNoError(t *testing.T) {
	log, err := readBuildOutput(strings.NewReader("plain text line, not json\n"))
	require.NoError(t, err)
	require.Contains(t, log, "plain text line")
}

func TestBuildError_FitsBudget(t *testing.T) {
	log := strings.Repeat("L", maxBuildLogBytes)
	err := buildError(errors.New("boom: build failed"), log)
	msg := err.Error()
	require.Contains(t, msg, "boom: build failed")
	require.Contains(t, msg, "build log tail:")
	require.True(t, len(msg) <= maxBuildLogBytes, "错误总长不得超过构建日志预算")
}

func TestTailBuffer_KeepsTail(t *testing.T) {
	var b tailBuffer
	for i := 0; i < maxBuildLogBytes/1024+1; i++ {
		_, _ = b.Write([]byte(strings.Repeat(string(rune('a'+i%26)), 1024)))
	}
	_, _ = b.Write([]byte(strings.Repeat("z", 1024)))
	got := b.String()
	require.Len(t, got, maxBuildLogBytes)
	require.True(t, strings.HasSuffix(got, strings.Repeat("z", 1024)))
	require.False(t, strings.HasPrefix(got, strings.Repeat("a", 1024)), "头部已被丢弃")
}
