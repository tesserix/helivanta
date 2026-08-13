package authz

import (
	"context"
	"encoding/json"
	"fmt"

	fgaclient "github.com/openfga/go-sdk/client"
)

// WriteModelForTest writes modelJSON as a new authorization model
// version, unconditionally — unlike ensureModel it does not compare
// against what is already there. It exists only to seed a store with a
// deliberately older model in tests, so TestEnsureModelUpgradesAnExistingStore
// can exercise the upgrade path against a store that looks the way a
// pre-existing production store would.
func (c *Client) WriteModelForTest(ctx context.Context, rawModelJSON string) error {
	var body fgaclient.ClientWriteAuthorizationModelRequest
	if err := json.Unmarshal([]byte(rawModelJSON), &body); err != nil {
		return fmt.Errorf("parse model: %w", err)
	}
	if _, err := c.api.WriteAuthorizationModel(ctx).Body(body).Execute(); err != nil {
		return fmt.Errorf("write model: %w", err)
	}
	return nil
}
