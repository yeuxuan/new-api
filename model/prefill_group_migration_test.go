package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigratePrefillGroupLegacyNameIndex(t *testing.T) {
	previousDB := DB
	previousType := common.MainDatabaseType()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB = previousDB
		common.SetMainDatabaseType(previousType)
	})

	require.NoError(t, db.AutoMigrate(&PrefillGroup{}))
	require.NoError(t, db.Migrator().DropIndex(&PrefillGroup{}, "uk_prefill_name"))
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX idx_prefill_groups_name ON prefill_groups(name)`).Error)
	require.True(t, db.Migrator().HasIndex(&PrefillGroup{}, legacyPrefillGroupNameIndex))

	require.NoError(t, MigratePrefillGroupLegacyNameIndex())
	require.NoError(t, MigratePrefillGroupLegacyNameIndex())
	assert.False(t, db.Migrator().HasIndex(&PrefillGroup{}, legacyPrefillGroupNameIndex))

	require.NoError(t, db.AutoMigrate(&PrefillGroup{}))
	require.True(t, db.Migrator().HasIndex(&PrefillGroup{}, "uk_prefill_name"))
	first := PrefillGroup{Name: "shared-name", Type: "model"}
	require.NoError(t, db.Create(&first).Error)
	require.NoError(t, db.Delete(&first).Error)
	require.NoError(t, db.Create(&PrefillGroup{Name: "shared-name", Type: "model"}).Error)
}
