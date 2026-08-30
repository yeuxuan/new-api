package model

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/constant"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const tmlabChannelTypeMigrationKey = "MigrationTMLabSeedanceChannelType62"

func migrateLegacyTMLabChannelType(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var marker Option
		err := tx.Where("key = ?", tmlabChannelTypeMigrationKey).Take(&marker).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		// Before task plugins were introduced, this fork exclusively used type 61
		// for TMLab Seedance. Move both live channels and historical tasks before
		// type 61 starts representing upstream task plugins.
		if err := tx.Model(&Channel{}).
			Where("type = ?", constant.ChannelTypeTaskPlugin).
			Update("type", constant.ChannelTypeTMLabSeedance).Error; err != nil {
			return err
		}
		if err := tx.Model(&Task{}).
			Where("platform = ?", strconv.Itoa(constant.ChannelTypeTaskPlugin)).
			Update("platform", strconv.Itoa(constant.ChannelTypeTMLabSeedance)).Error; err != nil {
			return err
		}

		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&Option{
			Key:   tmlabChannelTypeMigrationKey,
			Value: "1",
		}).Error
	})
}
