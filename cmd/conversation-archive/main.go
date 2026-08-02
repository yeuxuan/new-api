package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	flag.StringVar(&cfg.mode, "mode", "verify", "operation: migrate, recover, verify, verify-online, reindex, or export")
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
		MountSentinel: common.ConversationLogStorageSentinel,
	})
	if err != nil {
		fatal(err)
	}
	maintenanceLock, err := store.AcquireMaintenanceLock()
	if err != nil {
		fatal(err)
	}
	releaseMaintenanceLock := func() {
		if err := maintenanceLock.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "conversation-archive: release maintenance lock:", err)
		}
	}
	defer releaseMaintenanceLock()

	switch cfg.mode {
	case "migrate":
		if cfg.checkpoint == "" {
			cfg.checkpoint = filepath.Join(cfg.root, "migration", "legacy-conversation-logs.checkpoint.json")
		}
		if err := validateArchiveOperationalPath(cfg.root, cfg.checkpoint, "migration", ".json"); err != nil {
			releaseMaintenanceLock()
			fatal(err)
		}
		err = migrateLegacy(cfg, store)
	case "verify":
		err = verifyArchives(cfg, store)
	case "verify-online":
		err = verifyOnlineArchives(cfg, store)
	case "recover":
		var recovered []conversationarchive.Result
		recovered, err = store.RecoverPending()
		if err == nil {
			fmt.Printf("recovered pending captures=%d\n", len(recovered))
			err = reindexArchives(store)
		}
	case "reindex":
		err = reindexArchives(store)
	case "export":
		if err := validateArchiveOperationalPath(cfg.root, cfg.output, "exports", ".gz"); err != nil {
			releaseMaintenanceLock()
			fatal(err)
		}
		err = exportArchives(cfg, store)
	default:
		err = fmt.Errorf("unsupported mode %q", cfg.mode)
	}
	if err != nil {
		releaseMaintenanceLock()
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
			expectedRecord := legacyRecord(legacy, archiveID)
			var existing model.ConversationArchive
			err := model.DB.Where("archive_id = ?", archiveID).First(&existing).Error
			switch {
			case err == nil:
				record, restored, restoreErr := store.Restore(existing.RecordPath)
				if restoreErr != nil || restored.SHA256 != existing.RecordSha256 {
					return fmt.Errorf("verify existing archive %s: restore_error=%v expected_sha=%s actual_sha=%s", archiveID, restoreErr, existing.RecordSha256, restored.SHA256)
				}
				if !sameLegacyRecord(record, expectedRecord) {
					return fmt.Errorf("existing archive %s does not match the current legacy source row", archiveID)
				}
			case !errors.Is(err, gorm.ErrRecordNotFound):
				return fmt.Errorf("lookup archive %s: %w", archiveID, err)
			default:
				result, writeErr := store.WriteDeterministic(expectedRecord)
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
	pending, err := store.PendingPaths()
	if err != nil {
		return fmt.Errorf("inspect unfinished archive captures: %w", err)
	}
	if len(pending) > 0 {
		return fmt.Errorf("found %d unfinished conversation capture(s), first=%s", len(pending), pending[0])
	}
	highWaterID, err := conversationArchiveHighWaterID()
	if err != nil {
		return err
	}
	verified, indexedPaths, err := verifyArchiveManifests(cfg, store, highWaterID)
	if err != nil {
		return err
	}

	recordPaths, err := store.RecordPaths()
	if err != nil {
		return fmt.Errorf("enumerate conversation archive records: %w", err)
	}
	for _, recordPath := range recordPaths {
		if _, indexed := indexedPaths[recordPath]; !indexed {
			return fmt.Errorf("conversation archive record has no database index: %s; run --mode reindex", recordPath)
		}
	}
	orphanBlobs, err := store.OrphanBlobPaths()
	if err != nil {
		return fmt.Errorf("inspect conversation archive blobs: %w", err)
	}
	if len(orphanBlobs) > 0 {
		return fmt.Errorf("found %d unreferenced conversation archive blob(s), first=%s", len(orphanBlobs), orphanBlobs[0])
	}
	transientPaths, err := store.TransientPaths()
	if err != nil {
		return fmt.Errorf("inspect conversation archive transient files: %w", err)
	}
	if len(transientPaths) > 0 {
		return fmt.Errorf("found %d interrupted atomic-write file(s), first=%s", len(transientPaths), transientPaths[0])
	}
	fmt.Printf("verified archives=%d\n", verified)
	return nil
}

// verifyOnlineArchives validates a fixed database high-water mark while the
// gateway continues accepting traffic. It deliberately does not treat active
// pending captures or newer immutable files as corruption; the strict verify
// mode remains the maintenance-window check for global orphan/transient files.
func verifyOnlineArchives(cfg options, store *conversationarchive.Store) error {
	highWaterID, err := conversationArchiveHighWaterID()
	if err != nil {
		return err
	}
	pending, err := store.PendingPaths()
	if err != nil {
		return fmt.Errorf("inspect active archive captures: %w", err)
	}
	verified, _, err := verifyArchiveManifests(cfg, store, highWaterID)
	if err != nil {
		return err
	}
	fmt.Printf("verified online archives=%d high_water_id=%d active_pending=%d\n", verified, highWaterID, len(pending))
	return nil
}

func conversationArchiveHighWaterID() (int64, error) {
	var highWaterID int64
	if err := model.DB.Model(&model.ConversationArchive{}).Select("COALESCE(MAX(id), 0)").Scan(&highWaterID).Error; err != nil {
		return 0, fmt.Errorf("snapshot conversation archive high-water id: %w", err)
	}
	return highWaterID, nil
}

func verifyArchiveManifests(cfg options, store *conversationarchive.Store, highWaterID int64) (int64, map[string]struct{}, error) {
	var afterID int64
	var verified int64
	indexedPaths := make(map[string]struct{})
	for {
		var manifests []model.ConversationArchive
		if err := model.DB.Where("id > ? AND id <= ?", afterID, highWaterID).Order("id ASC").Limit(cfg.batchSize).Find(&manifests).Error; err != nil {
			return 0, nil, fmt.Errorf("read archive manifests: %w", err)
		}
		if len(manifests) == 0 {
			break
		}
		for _, manifest := range manifests {
			record, result, err := store.Restore(manifest.RecordPath)
			if err != nil {
				return 0, nil, fmt.Errorf("restore archive %s: %w", manifest.ArchiveId, err)
			}
			if result.SHA256 != manifest.RecordSha256 {
				return 0, nil, fmt.Errorf("archive %s checksum mismatch: manifest=%s restored=%s", manifest.ArchiveId, manifest.RecordSha256, result.SHA256)
			}
			expected := manifestFromRecord(record, result)
			if !sameArchiveManifest(manifest, expected) {
				return 0, nil, fmt.Errorf("archive %s database manifest does not match immutable record; manual review is required because reindex only rebuilds missing rows", manifest.ArchiveId)
			}
			afterID = manifest.Id
			verified++
			indexedPaths[filepath.ToSlash(filepath.Clean(manifest.RecordPath))] = struct{}{}
		}
	}
	return verified, indexedPaths, nil
}

func reindexArchives(store *conversationarchive.Store) error {
	recordPaths, err := store.RecordPaths()
	if err != nil {
		return err
	}
	var inserted int64
	for _, recordPath := range recordPaths {
		record, result, err := store.Restore(recordPath)
		if err != nil {
			return fmt.Errorf("restore archive %s: %w", recordPath, err)
		}
		var existing model.ConversationArchive
		err = model.DB.Where("archive_id = ?", record.ID).First(&existing).Error
		if err == nil {
			expected := manifestFromRecord(record, result)
			if filepath.ToSlash(filepath.Clean(existing.RecordPath)) != recordPath || existing.RecordSha256 != result.SHA256 ||
				!sameArchiveManifest(existing, expected) {
				return fmt.Errorf("archive index conflict for %s", record.ID)
			}
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		manifest := manifestFromRecord(record, result)
		if err := model.InsertConversationArchive(&manifest); err != nil {
			return fmt.Errorf("reindex archive %s: %w", record.ID, err)
		}
		inserted++
	}
	fmt.Printf("reindexed archives=%d scanned=%d\n", inserted, len(recordPaths))
	return nil
}

func manifestFromRecord(record conversationarchive.Record, result conversationarchive.Result) model.ConversationArchive {
	metadata := record.Metadata
	createdAt := record.RecordedAt.Unix()
	completedAt := createdAt
	if parsed, err := time.Parse(time.RFC3339Nano, metadata["completed_at"]); err == nil {
		completedAt = parsed.Unix()
	}
	channelID, _ := strconv.Atoi(metadata["channel_id"])
	userID, _ := strconv.Atoi(metadata["user_id"])
	tokenID, _ := strconv.Atoi(metadata["token_id"])
	statusCode, _ := strconv.Atoi(metadata["client_status"])
	if statusCode == 0 && record.Protocol == "legacy_conversation_log" {
		statusCode = http.StatusOK
	}
	isStream, _ := strconv.ParseBool(metadata["is_stream"])
	originModel := metadata["origin_model"]
	if originModel == "" {
		originModel = metadata["model"]
	}
	trainingConsent := metadata["training_consent"]
	if trainingConsent == "" {
		trainingConsent = "unknown"
	}
	requestBytes := int64(len(record.Payloads["client_request"]))
	responseBytes := int64(len(record.Payloads["client_response"]))
	if record.Protocol == "legacy_conversation_log" {
		requestBytes = int64(len(record.Payloads["legacy_messages"]))
		responseBytes = int64(len(record.Payloads["legacy_response"]))
	}
	return model.ConversationArchive{
		ArchiveId:            record.ID,
		RequestId:            metadata["request_id"],
		UserId:               userID,
		Username:             metadata["username"],
		TokenId:              tokenID,
		TokenName:            metadata["token_name"],
		ChannelId:            channelID,
		OriginModelName:      originModel,
		UpstreamModelName:    metadata["upstream_model"],
		ClientProtocol:       record.Protocol,
		UpstreamProtocol:     metadata["upstream_protocol"],
		RequestPath:          metadata["request_path"],
		CreatedAt:            createdAt,
		CompletedAt:          completedAt,
		StatusCode:           statusCode,
		IsStream:             isStream,
		Complete:             record.Completeness.Complete && len(record.Completeness.Missing) == 0,
		TrainingConsent:      trainingConsent,
		RecordPath:           result.RecordPath,
		RecordSha256:         result.SHA256,
		RequestBytes:         requestBytes,
		ResponseBytes:        responseBytes,
		StoredBytes:          result.StoredBytes,
		RequestConversion:    metadata["request_conversion_chain"],
		ArchiveFormatVersion: conversationarchive.CurrentVersion,
		ErrorCode:            metadata["relay_error_code"],
	}
}

func sameArchiveManifest(actual, expected model.ConversationArchive) bool {
	actual.Id = 0
	expected.Id = 0
	return actual == expected
}

func exportArchives(cfg options, store *conversationarchive.Store) (returnErr error) {
	if cfg.output == "" || filepath.Ext(cfg.output) != ".gz" {
		return errors.New("export mode requires an --output path ending in .gz")
	}
	if _, err := os.Lstat(cfg.output); err == nil {
		return fmt.Errorf("refusing to overwrite existing export: %s", cfg.output)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := verifyArchives(cfg, store); err != nil {
		return fmt.Errorf("pre-export archive verification: %w", err)
	}
	var highWaterID int64
	if err := model.DB.Model(&model.ConversationArchive{}).Select("COALESCE(MAX(id), 0)").Scan(&highWaterID).Error; err != nil {
		return fmt.Errorf("snapshot archive export high-water id: %w", err)
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
		if err := store.CheckWritable(0); err != nil {
			return err
		}
		var manifests []model.ConversationArchive
		if err := model.DB.Where("id > ? AND id <= ?", afterID, highWaterID).Order("id ASC").Limit(cfg.batchSize).Find(&manifests).Error; err != nil {
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
			if result.SHA256 != manifest.RecordSha256 || !sameArchiveManifest(manifest, manifestFromRecord(record, result)) {
				return fmt.Errorf("archive %s changed after pre-export verification", manifest.ArchiveId)
			}
			row, err := common.Marshal(trainingExportRow{
				Version:      1,
				ArchiveID:    manifest.ArchiveId,
				Protocol:     record.Protocol,
				RecordedAt:   record.RecordedAt,
				Complete:     record.Completeness.Complete && len(record.Completeness.Missing) == 0,
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
	if err := store.CheckWritable(0); err != nil {
		return err
	}
	if err := os.Link(tempName, cfg.output); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("refusing to overwrite existing export: %s", cfg.output)
		}
		return err
	}
	if err := os.Remove(tempName); err != nil {
		return fmt.Errorf("remove published export temporary link: %w", err)
	}
	directory, err := os.Open(filepath.Dir(cfg.output))
	if err != nil {
		return err
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return err
	}
	if err := directory.Close(); err != nil {
		return err
	}
	fmt.Printf("exported archives=%d high_water_id=%d output=%s\n", exported, highWaterID, cfg.output)
	return nil
}

func sameLegacyRecord(actual, expected conversationarchive.Record) bool {
	if actual.ID != expected.ID || !actual.RecordedAt.Equal(expected.RecordedAt) || actual.Protocol != expected.Protocol ||
		actual.Completeness.Complete != expected.Completeness.Complete || len(actual.Completeness.Missing) != len(expected.Completeness.Missing) ||
		len(actual.Metadata) != len(expected.Metadata) || len(actual.Payloads) != len(expected.Payloads) {
		return false
	}
	for index, missing := range actual.Completeness.Missing {
		if expected.Completeness.Missing[index] != missing {
			return false
		}
	}
	for key, value := range expected.Metadata {
		if actual.Metadata[key] != value {
			return false
		}
	}
	for key, value := range expected.Payloads {
		if !bytes.Equal(actual.Payloads[key], value) {
			return false
		}
	}
	return true
}

func validateArchiveOperationalPath(root, path, namespace, suffix string) error {
	if path == "" {
		return fmt.Errorf("path under %s is required", namespace)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	allowedRoot := filepath.Join(absRoot, namespace)
	relative, err := filepath.Rel(allowedRoot, absPath)
	if err != nil || !filepath.IsLocal(relative) || relative == "." {
		return fmt.Errorf("path must be a file below %s", allowedRoot)
	}
	if suffix != "" && !strings.HasSuffix(strings.ToLower(absPath), suffix) {
		return fmt.Errorf("path must end in %s", suffix)
	}
	current := allowedRoot
	parent := filepath.Dir(absPath)
	for {
		info, statErr := os.Lstat(current)
		if statErr == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("archive operational path contains a non-directory or symbolic link: %s", current)
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		if current == parent {
			break
		}
		nextRelative, err := filepath.Rel(current, parent)
		if err != nil || !filepath.IsLocal(nextRelative) {
			return fmt.Errorf("path must remain below %s", allowedRoot)
		}
		nextPart := strings.Split(nextRelative, string(filepath.Separator))[0]
		current = filepath.Join(current, nextPart)
	}
	if info, statErr := os.Lstat(absPath); statErr == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive operational file must be a regular file, not a symbolic link: %s", absPath)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
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
