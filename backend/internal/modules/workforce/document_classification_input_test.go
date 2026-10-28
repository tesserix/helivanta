package workforce

import (
	"encoding/json"
	"github.com/gin-gonic/gin/binding"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDocumentClassificationInputAcceptsZeroAndExplicitClear(t *testing.T) {
	for _, reference := range []string{"", "11111111-1111-4111-8111-111111111111"} {
		var input documentClassificationInput
		require.NoError(t, json.Unmarshal([]byte(`{"expected_version":0,"classification_revision_id":"`+reference+`"}`), &input))
		require.NoError(t, binding.Validator.ValidateStruct(&input))
	}
}
