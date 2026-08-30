package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
)

const legacyPrefillGroupNameIndex = "idx_prefill_groups_name"

// MigratePrefillGroupLegacyNameIndex removes the pre-soft-delete uniqueness rule.
func MigratePrefillGroupLegacyNameIndex() error {
	if DB == nil || !DB.Migrator().HasTable(&PrefillGroup{}) {
		return nil
	}

	migrator := DB.Migrator()
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) && migrator.HasConstraint(&PrefillGroup{}, legacyPrefillGroupNameIndex) {
		if err := migrator.DropConstraint(&PrefillGroup{}, legacyPrefillGroupNameIndex); err != nil {
			return fmt.Errorf("drop legacy prefill group name constraint: %w", err)
		}
	}
	if migrator.HasIndex(&PrefillGroup{}, legacyPrefillGroupNameIndex) {
		if err := migrator.DropIndex(&PrefillGroup{}, legacyPrefillGroupNameIndex); err != nil {
			return fmt.Errorf("drop legacy prefill group name index: %w", err)
		}
	}
	return nil
}
