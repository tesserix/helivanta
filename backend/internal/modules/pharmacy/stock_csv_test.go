package pharmacy

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/csvsafe"
)

func TestStockCSVRefusesOversizedEncodingWithoutReturningPartialBytes(t *testing.T) {
	data, err := encodeStockCSV([]stockPosition{{BrandName: strings.Repeat("TEST", 3*1024*1024)}}, time.Unix(0, 0))
	require.ErrorIs(t, err, csvsafe.ErrTooLarge)
	require.Nil(t, data)
}

func TestStockCSVProtectsUntrustedTextCells(t *testing.T) {
	data, err := encodeStockCSV([]stockPosition{{BrandName: "=1+1", LotNumber: "@SUM(1)", SKU: "00001"}}, time.Unix(0, 0))
	require.NoError(t, err)
	row := strings.Split(string(data), "\r\n")[1]
	require.Contains(t, row, ",'=1+1,")
	require.Contains(t, row, ",'@SUM(1),")
	require.Contains(t, row, ",00001,")
}
