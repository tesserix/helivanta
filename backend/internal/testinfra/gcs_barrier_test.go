package testinfra

import (
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReleasedReadBarrierHoldsExactlyTheRequestedConcurrentReads(t *testing.T) {
	cloud := NewGCSProtocol(t)
	cloud.objects[cloud.Released+"/synthetic-barrier"] = protocolObject{bytes: []byte("synthetic barrier bytes"), generation: 1}
	paused, resume := make(chan struct{}, 2), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	defer release()
	cloud.AfterReleasedReads(2, func() { paused <- struct{}{}; <-resume })
	responses := make(chan error, 2)
	read := func() error {
		reader, err := cloud.Client.Bucket(cloud.Released).Object("synthetic-barrier").NewReader(t.Context())
		if err != nil {
			return err
		}
		data, err := io.ReadAll(reader)
		if closeErr := reader.Close(); err == nil {
			err = closeErr
		}
		if err == nil && string(data) != "synthetic barrier bytes" {
			return errors.New("unexpected synthetic media bytes")
		}
		return err
	}
	for range 2 {
		go func() { responses <- read() }()
	}
	for range 2 {
		select {
		case <-paused:
		case err := <-responses:
			t.Fatalf("read escaped the configured barrier: %v", err)
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		}
	}
	require.NoError(t, read(), "only the configured number of media reads is held")
	select {
	case err := <-responses:
		t.Fatalf("held read returned before release: %v", err)
	default:
	}
	release()
	for range 2 {
		require.NoError(t, <-responses)
	}
}
