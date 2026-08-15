package claudeweb

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConvertStreamSupportsEventTypeOutsidePayload(t *testing.T) {
	source := strings.NewReader(strings.Join([]string{
		"event: content_block_start",
		`data: {"index":0,"content_block":{"type":"text","text":""}}`,
		"",
		"event: content_block_delta",
		`data: {"index":0,`,
		`data: "delta":{"type":"text_delta","text":"Hello"}}`,
		"",
		"event: content_block_stop",
		`data: {"index":0}`,
		"",
	}, "\n"))

	var destination bytes.Buffer
	usage, err := ConvertStream(context.Background(), source, &destination, DefaultModel, 11)
	require.NoError(t, err)
	require.Equal(t, 11, usage.InputTokens)
	require.Positive(t, usage.OutputTokens)
	require.Contains(t, destination.String(), `"type":"content_block_start"`)
	require.Contains(t, destination.String(), `"type":"content_block_delta"`)
	require.Contains(t, destination.String(), `"text":"Hello"`)
	require.Contains(t, destination.String(), "event: message_stop")
}

func TestConvertStreamPropagatesUpstreamErrorEvent(t *testing.T) {
	source := strings.NewReader("event: error\ndata: {\"error\":{\"message\":\"session expired\"}}\n\n")

	var destination bytes.Buffer
	_, err := ConvertStream(context.Background(), source, &destination, DefaultModel, 0)
	require.EqualError(t, err, "session expired")
}
