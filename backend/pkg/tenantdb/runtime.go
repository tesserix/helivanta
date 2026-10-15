package tenantdb

import (
	"errors"
	"gorm.io/gorm"
)

var errNoSchemaPool = errors.New("tenantdb: runtime connection has no schema-owner pool")

// Close releases every pool owned by this connection set.
func (d *DB) Close() error {
	var errs []error
	for _, g := range []*gorm.DB{d.app, d.admin, d.system} {
		if g == nil {
			continue
		}
		pool, err := g.DB()
		if err == nil {
			err = pool.Close()
		}
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
