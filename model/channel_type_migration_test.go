package model

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigrateLegacyTMLabChannelType(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:tmlab-channel-type-migration?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}, &Channel{}, &Task{}))

	legacyChannel := Channel{
		Type: constant.ChannelTypeTaskPlugin,
		Key:  "legacy-tmlab-key",
		Name: "legacy-tmlab",
	}
	require.NoError(t, db.Create(&legacyChannel).Error)
	legacyTask := Task{
		TaskID:    "legacy-tmlab-task",
		Platform:  constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeTaskPlugin)),
		ChannelId: legacyChannel.Id,
	}
	require.NoError(t, db.Create(&legacyTask).Error)

	require.NoError(t, migrateLegacyTMLabChannelType(db))

	var migratedChannel Channel
	require.NoError(t, db.First(&migratedChannel, legacyChannel.Id).Error)
	assert.Equal(t, constant.ChannelTypeTMLabSeedance, migratedChannel.Type)

	var migratedTask Task
	require.NoError(t, db.First(&migratedTask, legacyTask.ID).Error)
	assert.Equal(t, constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeTMLabSeedance)), migratedTask.Platform)

	var marker Option
	require.NoError(t, db.Where("key = ?", tmlabChannelTypeMigrationKey).Take(&marker).Error)
	assert.Equal(t, "1", marker.Value)
}

func TestMigrateLegacyTMLabChannelTypeRunsOnlyOnce(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:tmlab-channel-type-migration-once?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}, &Channel{}, &Task{}))
	require.NoError(t, migrateLegacyTMLabChannelType(db))

	pluginChannel := Channel{
		Type: constant.ChannelTypeTaskPlugin,
		Key:  "task-plugin-key",
		Name: "task-plugin",
	}
	require.NoError(t, db.Create(&pluginChannel).Error)
	pluginTask := Task{
		TaskID:    "task-plugin-task",
		Platform:  constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeTaskPlugin)),
		ChannelId: pluginChannel.Id,
	}
	require.NoError(t, db.Create(&pluginTask).Error)

	require.NoError(t, migrateLegacyTMLabChannelType(db))

	var storedChannel Channel
	require.NoError(t, db.First(&storedChannel, pluginChannel.Id).Error)
	assert.Equal(t, constant.ChannelTypeTaskPlugin, storedChannel.Type)

	var storedTask Task
	require.NoError(t, db.First(&storedTask, pluginTask.ID).Error)
	assert.Equal(t, constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeTaskPlugin)), storedTask.Platform)
}
