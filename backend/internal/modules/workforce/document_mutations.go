package workforce

import "gorm.io/gorm"

type documentMutationStage string

const (
	documentMutationIntent documentMutationStage = "intent"
	documentMutationStamp  documentMutationStage = "stamp"
	documentMutationQueue  documentMutationStage = "queue"
	documentMutationRevoke documentMutationStage = "revoke"
	documentMutationRetry  documentMutationStage = "retry"
)

type documentMutation struct {
	Stage      documentMutationStage
	Input      *documentInput
	Saved      *document
	Generation int64
	Attempt    int64
}

func prepareDocumentMutation(tx *gorm.DB, gate documentGate, mutation documentMutation) error {
	if selected, ok := gate.(interface {
		prepareMutation(*gorm.DB, documentMutation) error
	}); ok {
		return selected.prepareMutation(tx, mutation)
	}
	return nil
}
