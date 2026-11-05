package workforce

import (
	"encoding/json"

	"gorm.io/gorm"
)

func savePostingSourceFrame(tx *gorm.DB, keys []string) (func() error, error) {
	names, err := json.Marshal(keys)
	if err != nil {
		return nil, err
	}
	var previous string
	if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM jsonb_array_elements_text(?::jsonb) AS key`, string(names)).Scan(&previous).Error; err != nil {
		return nil, err
	}
	return func() error {
		return tx.Exec(`SELECT set_config(key,value,true) FROM jsonb_each_text(?::jsonb)`, previous).Error
	}, nil
}
