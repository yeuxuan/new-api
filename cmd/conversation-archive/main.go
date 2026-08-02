package main

import (
	"bufio"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/conversationarchive"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

type options struct {
	mode       string
	root       string
	checkpoint string
	output     string
	batchSize  int
	highWater  int64
}

type migrationCheckpoint struct {
	Version        int       `json:"version"`
	SourceTable    string    `json:"source_table"`
	HighWaterID    int64     `json:"high_water_id"`
	LastArchivedID int64     `json:"last_archived_id"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type trainingExportRow struct {
	Version      int               `json:"version"`
	ArchiveID    string            `json:"archive_id"`
	Protocol     string            `json:"protocol"`
	RecordedAt   time.Time         `json:"recorded_at"`
	Complete     bool              `json:"complete"`
	Missing      []string          `json:"missing,omitempty"`
	Metadata     map[string]string `json:"metadata"`
	Payloads     map[string][]byte `json:"payloads_base64"`
	RecordSHA256 string            `json:"record_sha256"`
}

func main() {
	var cfg options
	flag.StringVar(&cfg.mode, "mode", "verify", "operation: migrate, verify, or export")
	flag.StringVar(&cfg.root, "root", "", "conversation archive root; defaults to CONVERSATION_LOG_STORAGE_PATH")
	flag.StringVar(&cfg.checkpoint, "checkpoint", "", "legacy migration checkpoint path")
	flag.StringVar(&cfg.output, "output", "", "export .jsonl.gz output path")
	flag.IntVar(&cfg.batchSize, "batch-size", 5, "legacy migration/query batch size")
	flag.Int64Var(&cfg.highWater, "high-water-id", 0, "fixed source high-water id; 0 snapshots MAX(id)")

	common.InitEnv()
	if cfg.root == "" {
		cfg.root = common.ConversationLogStoragePath
	}
	if cfg.root == "" {
		fatal(errors.New("conversation archive root is required"))
	}
	if cfg.batchSize < 1 || cfg.batchSize > 100 {
		fatal(errors.New("batch-size must be between 1 and 100"))
	}

	// The running gateway owns schema migration. This utility opens the same
	// database read/write for archive indexes without racing AutoMigrate.
	common.IsMasterNode = false
	if err := model.InitDB(); err != nil {
		fatal(fmt.Errorf("initialize database: %w", err))
	}
	if sqlDB, err := model.DB.DB(); err == nil {
		defer func() { _ = sqlDB.Close() }()
	}

	store, err := conversationarchive.New(cfg.root, conversationarchive.Options{
		BlobThreshold: common.ConversationLogBlobThreshold,
		MinFreeBytes:  common.ConversationLogMinFreeBytes,
	})
	if err != nil {
		fatal(err)
	}

	switch cfg.mode {
	case "migrate":
		if cfg.checkpoint == "" {
			cfg.checkpoint = filepath.Join(cfg.root, "migration", "legacy-conversation-logs.checkpoint.json")
		}
		err = migrateLegacy(cfg, store)
	case "verify":
		err = verifyArchives(cfg, store)
	case "export":
		err = exportArchives(cfg, store)
	default:
		err = fmt.Errorf("unsupported mode %q", cfg.mode)
	}
	if err != nil {
		fatal(err)
	}
}

func migrateLegacy(cfg options, store *conversationarchive.Store) error {
	checkpoint, err := loadCheckpoint(cfg.checkpoint)
	if err != nil {
		return err
	}
	if checkpoint.HighWaterID == 0 {
		checkpoint.HighWaterID = cfg.highWater
		if checkpoint.HighWaterID == 0 {
			if err := model.DB.Model(&model.ConversationLog{}).Select("COALESCE(MAX(id), 0)").Scan(&checkpoint.HighWaterID).Error; err != nil {
				return fmt.Errorf("snapshot legacy high-water id: %w", err)
			}
		}
		checkpoint.Version = 1
		checkpoint.SourceTable = "conversation_logs"
		if err := saveCheckpoint(cfg.checkpoint, checkpoint); err != nil {
			return err
		}
	} else if cfg.highWater != 0 && cfg.highWater != checkpoint.HighWaterID {
		return fmt.Errorf("high-water-id %d does not match existing checkpoint %d", cfg.highWater, checkpoint.HighWaterID)
	}

	for checkpoint.LastArchivedID < checkpoint.HighWaterID {
		var logs []model.ConversationLog
		if err := model.DB.Where("id > ? AND id <= ?", checkpoint.LastArchivedID, checkpoint.HighWaterID).
			Order("id ASC").Limit(cfg.batchSize).Find(&logs).Error; err != nil {
			return fmt.Errorf("read legacy conversation logs: %w", err)
		}
		if len(logs) == 0 {
			return fmt.Errorf("legacy source gap after id %d before high-water id %d", checkpoint.LastArchivedID, checkpoint.HighWaterID)
		}
		for _, legacy := range logs {
			archiveID := "legacy-" + strconv.FormatInt(legacy.Id, 10)
			var existing model.ConversationArchive
			err := model.DB.Where("archive_id = ?", archiveID).First(&existing).Error
			switch {
			case err == nil:
				if _, restored, restoreErr := store.Restore(existing.RecordPath); restoreErr != nil || restored.SHA256 != existing.RecordSha256 {
					return fmt.Errorf("verify existing archive %s: restore_error=%v expected_sha=%s actual_sha=%s", archiveID, restoreErr, existing.RecordSha256, restored.SHA256)
				}
			case !errors.Is(err, gorm.ErrRecordNotFound):
				return fmt.Errorf("lookup archive %s: %w", archiveID, err)
			default:
				result, writeErr := store.Write(legacyRecord(legacy, archiveID))
				if writeErr != nil {
					return fmt.Errorf("archive legacy conversation id %d: %w", legacy.Id, writeErr)
				}
				manifest := legacyManifest(legacy, archiveID, result)
				if insertErr := model.InsertConversationArchive(&manifest); insertErr != nil {
					return fmt.Errorf("index legacy conversation id %d: %w", legacy.Id, insertErr)
				}
			}
			checkpoint.LastArchivedID = legacy.Id
			if err := saveCheckpoint(cfg.checkpoint, checkpoint); err != nil {
				return err
			}
		}
		fmt.Printf("archived legacy conversation id=%d high_water=%d\n", checkpoint.LastArchivedID, checkpoint.HighWaterID)
	}
	return nil
}

func legacyRecord(legacy model.ConversationLog, archiveID string) conversationarchive.Record {
	return conversationarchive.Record{
		ID:         archiveID,
		RecordedAt: time.Unix(legacy.CreatedAt, 0).UTC(),
		Protocol:   "legacy_conversation_log",
		Payloads: map[string][]byte{
			"legacy_messages": []byte(legacy.Messages),
			"legacy_response": []byte(legacy.Response),
		},
		Metadata: map[string]string{
			"legacy_id":    strconv.FormatInt(legacy.Id, 10),
			"request_id":   legacy.RequestId,
			"user_id":      strconv.Itoa(legacy.UserId),
			"username":     legacy.Username,
			"token_id":     strconv.Itoa(legacy.TokenId),
			"token_name":   legacy.TokenName,
			"model":        legacy.ModelName,
			"channel_id":   strconv.Itoa(legacy.ChannelId),
			"source_table": "conversation_logs",
		},
		Completeness: conversationarchive.Completeness{
			Complete: false,
			Missing:  []string{"original_client_request", "upstream_attempts", "failure_responses"},
		},
	}
}

func legacyManifest(legacy model.ConversationLog, archiveID string, result conversationarchive.Result) model.ConversationArchive {
	return model.ConversationArchive{
		ArchiveId:            archiveID,
		RequestId:            legacy.RequestId,
		UserId:               legacy.UserId,
		Username:             legacy.Username,
		TokenId:              legacy.TokenId,
		TokenName:            legacy.TokenName,
		ChannelId:            legacy.ChannelId,
		OriginModelName:      legacy.ModelName,
		ClientProtocol:       "legacy_conversation_log",
		CreatedAt:            legacy.CreatedAt,
		CompletedAt:          legacy.CreatedAt,
		StatusCode:           200,
		Complete:             false,
		TrainingConsent:      "unknown",
		RecordPath:           result.RecordPath,
		RecordSha256:         result.SHA256,
		RequestBytes:         int64(len(legacy.Messages)),
		ResponseBytes:        int64(len(legacy.Response)),
		StoredBytes:          result.StoredBytes,
		ArchiveFormatVersion: conversationarchive.CurrentVersion,
	}
}

func verifyArchives(cfg options, store *conversationarchive.Store) error {
	var afterID int64
	var verified int64
	for {
		var manifests []model.ConversationArchive
		if err := model.DB.Where("id > ?", afterID).Order("id ASC").Limit(cfg.batchSize).Find(&manifests).Error; err != nil {
			return fmt.Errorf("read archive manifests: %w", err)
		}
		if len(manifests) == 0 {
			break
		}
		for _, manifest := range manifests {
			_, result, err := store.Restore(manifest.RecordPath)
			if err != nil {
				return fmt.Errorf("restore archive %s: %w", manifest.ArchiveId, err)
			}
			if result.SHA256 != manifest.RecordSha256 {
				return fmt.Errorf("archive %s checksum mismatch: manifest=%s restored=%s", manifest.ArchiveId, manifest.RecordSha256, result.SHA256)
			}
			afterID = manifest.Id
			verified++
		}
	}
	fmt.Printf("verified archives=%d\n", verified)
	return nil
}

func exportArchives(cfg options, store *conversationarchive.Store) (returnErr error) {
	if cfg.output == "" || filepath.Ext(cfg.output) != ".gz" {
		return errors.New("export mode requires an --output path ending in .gz")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.output), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(cfg.output), ".conversation-export-*.partial")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		if returnErr != nil {
			_ = os.Remove(tempName)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	gzipWriter := gzip.NewWriter(temp)
	writer := bufio.NewWriterSize(gzipWriter, 1<<20)

	var afterID int64
	var exported int64
	for {
		var manifests []model.ConversationArchive
		if err := model.DB.Where("id > ?", afterID).Order("id ASC").Limit(cfg.batchSize).Find(&manifests).Error; err != nil {
			return err
		}
		if len(manifests) == 0 {
			break
		}
		for _, manifest := range manifests {
			record, result, err := store.Restore(manifest.RecordPath)
			if err != nil {
				return fmt.Errorf("restore archive %s: %w", manifest.ArchiveId, err)
			}
			row, err := common.Marshal(trainingExportRow{
				Version:      1,
				ArchiveID:    manifest.ArchiveId,
				Protocol:     record.Protocol,
				RecordedAt:   record.RecordedAt,
				Complete:     record.Completeness.Complete,
				Missing:      record.Completeness.Missing,
				Metadata:     record.Metadata,
				Payloads:     record.Payloads,
				RecordSHA256: result.SHA256,
			})
			if err != nil {
				return err
			}
			if _, err := writer.Write(row); err != nil {
				return err
			}
			if err := writer.WriteByte('\n'); err != nil {
				return err
			}
			afterID = manifest.Id
			exported++
		}
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	if err := gzipWriter.Close(); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, cfg.output); err != nil {
		return err
	}
	fmt.Printf("exported archives=%d output=%s\n", exported, cfg.output)
	return nil
}

func loadCheckpoint(path string) (migrationCheckpoint, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return migrationCheckpoint{}, nil
	}
	if err != nil {
		return migrationCheckpoint{}, err
	}
	var checkpoint migrationCheckpoint
	if err := common.Unmarshal(data, &checkpoint); err != nil {
		return migrationCheckpoint{}, fmt.Errorf("decode migration checkpoint: %w", err)
	}
	if checkpoint.Version != 1 || checkpoint.SourceTable != "conversation_logs" || checkpoint.LastArchivedID > checkpoint.HighWaterID {
		return migrationCheckpoint{}, errors.New("invalid legacy migration checkpoint")
	}
	return checkpoint, nil
}

func saveCheckpoint(path string, checkpoint migrationCheckpoint) (returnErr error) {
	checkpoint.UpdatedAt = time.Now().UTC()
	data, err := common.Marshal(checkpoint)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".checkpoint-*.partial")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		if returnErr != nil {
			_ = os.Remove(tempName)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "conversation-archive:", err)
	os.Exit(1)
}
