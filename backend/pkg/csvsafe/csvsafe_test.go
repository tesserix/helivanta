package csvsafe_test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/csvsafe"
)

func TestTextPrefixesEveryFormulaLikeValue(t *testing.T) {
	for _, value := range []string{"=1+1", "+cmd", "-cmd", "@SUM(1)", " \t=1", "\r\n=1", "\ufeff=1", "\x00=1", "\tTEST"} {
		t.Run(fmt.Sprintf("%q", value), func(t *testing.T) {
			require.Equal(t, "'"+value, csvsafe.Text(value))
		})
	}
	for _, value := range []string{"TEST normal", "00001", `TEST "quoted", name`, "", "🌡 TEST", "A+B"} {
		require.Equal(t, value, csvsafe.Text(value))
	}
}

func TestWriterEmitsCRLFRecordsWithStandardQuoting(t *testing.T) {
	w := csvsafe.NewWriter(1024)
	require.NoError(t, w.Write([]string{"code", "name"}))
	require.NoError(t, w.Write([]string{"SYN-1", `TEST "quoted", name`}))
	data, err := w.Bytes()
	require.NoError(t, err)
	require.Equal(t, "code,name\r\nSYN-1,\"TEST \"\"quoted\"\", name\"\r\n", string(data))
}

func TestWriterAcceptsExactlyTheLimit(t *testing.T) {
	w := csvsafe.NewWriter(len("abcd\r\n"))
	require.NoError(t, w.Write([]string{"abcd"}))
	data, err := w.Bytes()
	require.NoError(t, err)
	require.Equal(t, "abcd\r\n", string(data))
}

// encode reports the first error whether the overflow surfaces from Write or Bytes.
func encode(limit int, records ...[]string) ([]byte, error) {
	w := csvsafe.NewWriter(limit)
	for _, record := range records {
		if err := w.Write(record); err != nil {
			return nil, err
		}
	}
	return w.Bytes()
}

func TestWriterRefusesOneByteOverTheLimitWithoutPartialBytes(t *testing.T) {
	data, err := encode(len("abcd\r\n")-1, []string{"abcd"})
	require.ErrorIs(t, err, csvsafe.ErrTooLarge)
	require.Nil(t, data)
}

func TestWriterRefusesALargeCellWithoutPartialBytes(t *testing.T) {
	data, err := encode(8*1024*1024, []string{"code"}, []string{strings.Repeat("TEST", 3*1024*1024)})
	require.ErrorIs(t, err, csvsafe.ErrTooLarge)
	require.Nil(t, data)
}

func TestWriterKeepsRefusingAfterAnOverflow(t *testing.T) {
	w := csvsafe.NewWriter(4)
	_ = w.Write([]string{"abcdef"})
	_, _ = w.Bytes()
	require.ErrorIs(t, w.Write([]string{"a"}), csvsafe.ErrTooLarge)
	data, err := w.Bytes()
	require.ErrorIs(t, err, csvsafe.ErrTooLarge)
	require.Nil(t, data)
}

func TestWriterNeverBuffersPastItsLimit(t *testing.T) {
	cell := strings.Repeat("TEST", 16*1024*1024)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := encode(1024*1024, []string{cell})
	runtime.ReadMemStats(&after)
	require.ErrorIs(t, err, csvsafe.ErrTooLarge)
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(8*1024*1024))
}
