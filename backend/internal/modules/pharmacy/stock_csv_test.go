package pharmacy

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStockCSVRefusesOversizedEncodingWithoutReturningPartialBytes(t *testing.T) {
	data, err := encodeStockCSV([]stockPosition{{BrandName: strings.Repeat("TEST", 3*1024*1024)}}, time.Unix(0, 0))
	require.ErrorIs(t, err, errStockCSVTooLarge)
	require.Nil(t, data)
}

func TestStockCSVProtectsAllUntrustedTextCells(t *testing.T) {
	for _, value := range []string{"=1+1", "+cmd", "-cmd", "@SUM(1)", " \t=1", "\r\n=1", "\ufeff=1", "\x00=1", "\tTEST"} {
		t.Run(fmt.Sprintf("%q", value), func(t *testing.T) {
			require.Equal(t, "'"+value, stockCSVText(value))
		})
	}
	for _, value := range []string{"TEST normal", "00001", `TEST "quoted", name`, "", "🌡 TEST", "A+B"} {
		require.Equal(t, value, stockCSVText(value))
	}
}
