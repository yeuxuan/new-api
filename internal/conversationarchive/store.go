package conversationarchive

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	// CurrentVersion is the on-disk envelope format written by this package.
	CurrentVersion = 1

	defaultLargeStringThreshold = 64 << 10
)

type IntegrityStatus string

const (
	IntegrityComplete   IntegrityStatus = "complete"
	IntegrityIncomplete IntegrityStatus = "incomplete"
)

// Options controls archive partitioning. A nil PartitionLocation uses UTC.
type Options struct {
	PartitionLocation *time.Location
	BlobThreshold     int
	MinFreeBytes      int64
	MountSentinel     string
}

// Completeness records whether every part of the conversation was captured.
// Missing should contain stable machine-readable descriptions of absent parts.
type Completeness struct {
	Complete bool     `json:"complete"`
	Missing  []string `json:"missing,omitempty"`
}

// Record is the lossless input and output contract for the archive. Payloads
// are arbitrary bytes, so JSON requests, SSE streams, and protocol-specific
// frames retain their original whitespace and escaping.
type Record struct {
	ID           string
	RecordedAt   time.Time
	Protocol     string
	Payloads     map[string][]byte
	Metadata     map[string]string
	Completeness Completeness
}

// Result contains the durable record locator and integrity data suitable for
// storing in a database index. StoredBytes is the compressed record size plus
// the compressed size of blobs referenced by this record; shared blobs are
// counted once per record.
type Result struct {
	RecordPath    string
	SHA256        string
	OriginalBytes int64
	StoredBytes   int64
	Integrity     IntegrityStatus
}

type Store struct {
	root          string
	location      *time.Location
	blobThreshold int
	minFreeBytes  int64
	mountSentinel string
	rename        func(string, string) error
	writeMu       sync.Mutex
}

// PendingPaths returns unfinished staging directories for operational checks.
func (s *Store) PendingPaths() ([]string, error) {
	if s == nil {
		return nil, errors.New("conversation archive store is nil")
	}
	entries, err := filepath.Glob(filepath.Join(s.root, "pending", "*.pending"))
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		info, err := os.Lstat(entry)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("conversation archive pending path is not a directory: %s", entry)
		}
		relative, err := filepath.Rel(s.root, entry)
		if err != nil {
			return nil, err
		}
		paths = append(paths, filepath.ToSlash(relative))
	}
	sort.Strings(paths)
	return paths, nil
}

// RecordPaths enumerates immutable envelopes without trusting the database
// index, allowing orphan detection and index reconstruction.
func (s *Store) RecordPaths() ([]string, error) {
	if s == nil {
		return nil, errors.New("conversation archive store is nil")
	}
	entries, err := filepath.Glob(filepath.Join(s.root, "records", "*", "*", "*", "*.json.gz"))
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		info, err := os.Lstat(entry)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("conversation archive record is not a regular file: %s", entry)
		}
		relative, err := filepath.Rel(s.root, entry)
		if err != nil {
			return nil, err
		}
		paths = append(paths, filepath.ToSlash(relative))
	}
	sort.Strings(paths)
	return paths, nil
}

// OrphanBlobPaths reports content-addressed blobs that are not referenced by
// any immutable envelope. It is intentionally read-only: operators decide
// whether a verified orphan is safe to remove.
func (s *Store) OrphanBlobPaths() ([]string, error) {
	recordPaths, err := s.RecordPaths()
	if err != nil {
		return nil, err
	}
	referenced := make(map[string]struct{})
	for _, recordPath := range recordPaths {
		absolutePath, err := s.secureRecordPath(recordPath)
		if err != nil {
			return nil, err
		}
		compressed, err := os.ReadFile(absolutePath)
		if err != nil {
			return nil, err
		}
		plain, err := gunzipBytes(compressed)
		if err != nil {
			return nil, fmt.Errorf("decompress conversation archive record %s: %w", recordPath, err)
		}
		var env envelope
		if err := common.Unmarshal(plain, &env); err != nil {
			return nil, fmt.Errorf("decode conversation archive record %s: %w", recordPath, err)
		}
		if env.Version != CurrentVersion {
			return nil, fmt.Errorf("unsupported conversation archive envelope version %d", env.Version)
		}
		values := make([]storedValue, 0, len(env.Payloads)+len(env.Metadata))
		for _, value := range env.Payloads {
			values = append(values, value)
		}
		for _, value := range env.Metadata {
			values = append(values, value)
		}
		for _, value := range values {
			for _, chunk := range value.Chunks {
				if chunk.Blob == nil {
					continue
				}
				if !validDigest(chunk.Blob.SHA256) {
					return nil, fmt.Errorf("record %s contains invalid blob reference", recordPath)
				}
				referenced[filepath.ToSlash(filepath.Join("blobs", "sha256", chunk.Blob.SHA256[:2], chunk.Blob.SHA256+".gz"))] = struct{}{}
			}
		}
	}

	blobs, err := filepath.Glob(filepath.Join(s.root, "blobs", "sha256", "*", "*.gz"))
	if err != nil {
		return nil, err
	}
	orphans := make([]string, 0)
	for _, blob := range blobs {
		info, err := os.Lstat(blob)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("conversation archive blob is not a regular file: %s", blob)
		}
		relative, err := filepath.Rel(s.root, blob)
		if err != nil {
			return nil, err
		}
		relative = filepath.ToSlash(relative)
		if _, found := referenced[relative]; !found {
			orphans = append(orphans, relative)
		}
	}
	sort.Strings(orphans)
	return orphans, nil
}

// TransientPaths reports atomic-write leftovers from interrupted processes.
// Active captures are reported separately through PendingPaths.
func (s *Store) TransientPaths() ([]string, error) {
	patterns := []struct {
		pattern       string
		directoryOnly bool
	}{
		{pattern: filepath.Join(s.root, "records", "*", "*", "*", ".conversation-archive-*.tmp")},
		{pattern: filepath.Join(s.root, "blobs", "sha256", "*", ".conversation-blob-*.tmp")},
		{pattern: filepath.Join(s.root, "blobs", "sha256", "*", ".conversation-archive-*.tmp")},
		{pattern: filepath.Join(s.root, "exports", ".conversation-export-*.partial")},
		{pattern: filepath.Join(s.root, "migration", ".checkpoint-*.partial")},
		{pattern: filepath.Join(s.root, "pending", ".*.pending.*-*"), directoryOnly: true},
		{pattern: filepath.Join(s.root, "pending", ".*.pending.lock")},
	}
	var paths []string
	for _, candidate := range patterns {
		matches, err := filepath.Glob(candidate.pattern)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			info, err := os.Lstat(match)
			if err != nil {
				return nil, err
			}
			if info.Mode()&os.ModeSymlink != 0 || (candidate.directoryOnly && !info.IsDir()) || (!candidate.directoryOnly && !info.Mode().IsRegular()) {
				return nil, fmt.Errorf("conversation archive transient path has an unexpected type: %s", match)
			}
			relative, err := filepath.Rel(s.root, match)
			if err != nil {
				return nil, err
			}
			paths = append(paths, filepath.ToSlash(relative))
		}
	}
	sort.Strings(paths)
	return paths, nil
}

type envelope struct {
	Version           int                    `json:"version"`
	ID                string                 `json:"id"`
	RecordedAt        time.Time              `json:"recorded_at"`
	Protocol          string                 `json:"protocol"`
	PartitionTimezone string                 `json:"partition_timezone"`
	Payloads          map[string]storedValue `json:"payloads,omitempty"`
	Metadata          map[string]storedValue `json:"metadata,omitempty"`
	Completeness      Completeness           `json:"completeness"`
	ContentSHA256     string                 `json:"content_sha256"`
	OriginalBytes     int64                  `json:"original_bytes"`
}

type storedValue struct {
	Size   int64         `json:"size"`
	SHA256 string        `json:"sha256"`
	Chunks []storedChunk `json:"chunks"`
}

type storedChunk struct {
	Kind   string   `json:"kind"`
	Inline []byte   `json:"inline,omitempty"`
	Blob   *blobRef `json:"blob,omitempty"`
}

type blobRef struct {
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	StoredSize int64  `json:"stored_size"`
}

var recordIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// New creates a filesystem-backed archive rooted at root.
func New(root string, options Options) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("conversation archive root is empty")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve conversation archive root: %w", err)
	}
	if unsafeArchiveRoot(absRoot) {
		return nil, fmt.Errorf("conversation archive root is too broad: %s", absRoot)
	}
	if err := validateSentinelName(options.MountSentinel); err != nil {
		return nil, err
	}
	rootExists := false
	if info, statErr := os.Lstat(absRoot); statErr == nil {
		rootExists = true
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("conversation archive root must not be a symbolic link: %s", absRoot)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("conversation archive root is not a directory: %s", absRoot)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect conversation archive root: %w", statErr)
	}
	if !rootExists {
		if options.MountSentinel != "" {
			return nil, fmt.Errorf("conversation archive root must already exist when a mount sentinel is configured: %s", absRoot)
		}
		if err := os.MkdirAll(absRoot, 0o700); err != nil {
			return nil, fmt.Errorf("create conversation archive root: %w", err)
		}
	}
	canonicalRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve conversation archive root symlinks: %w", err)
	}
	if err := os.Chmod(canonicalRoot, 0o700); err != nil {
		return nil, fmt.Errorf("secure conversation archive root: %w", err)
	}
	location := options.PartitionLocation
	if location == nil {
		location = time.UTC
	}
	blobThreshold := options.BlobThreshold
	if blobThreshold <= 0 {
		blobThreshold = defaultLargeStringThreshold
	}
	store := &Store{
		root:          canonicalRoot,
		location:      location,
		blobThreshold: blobThreshold,
		minFreeBytes:  options.MinFreeBytes,
		mountSentinel: options.MountSentinel,
		rename:        os.Rename,
	}
	if err := store.checkMountSentinel(); err != nil {
		return nil, err
	}
	return store, nil
}

// CheckWritable verifies that the archive filesystem retains its configured
// safety reserve after the estimated write. It lets the gateway reject a new
// conversation before contacting an upstream instead of filling the volume.
func (s *Store) CheckWritable(estimatedBytes int64) error {
	if s == nil {
		return errors.New("conversation archive store is nil")
	}
	if estimatedBytes < 0 {
		return errors.New("conversation archive estimated bytes cannot be negative")
	}
	if err := s.checkMountSentinel(); err != nil {
		return err
	}
	if s.minFreeBytes <= 0 {
		return nil
	}
	available, err := availableBytes(s.root)
	if err != nil {
		return fmt.Errorf("inspect conversation archive free space: %w", err)
	}
	if available >= 0 && (available < estimatedBytes || available-estimatedBytes < s.minFreeBytes) {
		return fmt.Errorf("conversation archive free space below reserve: available=%d estimated_write=%d reserve=%d", available, estimatedBytes, s.minFreeBytes)
	}
	return nil
}

func (s *Store) checkMountSentinel() error {
	if s.mountSentinel == "" {
		return nil
	}
	info, err := os.Lstat(filepath.Join(s.root, s.mountSentinel))
	if err != nil {
		return fmt.Errorf("conversation archive mount sentinel is unavailable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("conversation archive mount sentinel is not a regular file")
	}
	return nil
}

func validateSentinelName(name string) error {
	if name == "" {
		return nil
	}
	if filepath.Base(name) != name || name == "." || name == ".." || strings.ContainsAny(name, "\x00\r\n") {
		return fmt.Errorf("invalid conversation archive sentinel %q", name)
	}
	return nil
}

func unsafeArchiveRoot(path string) bool {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	remainder := strings.TrimPrefix(clean, volume)
	parts := strings.FieldsFunc(remainder, func(r rune) bool { return r == '/' || r == '\\' })
	return filepath.Dir(clean) == clean || len(parts) < 2
}

// Write atomically persists one versioned envelope and any content-addressed
// blobs. A failed record rename never exposes a partial record file.
func (s *Store) Write(record Record) (Result, error) {
	return s.write(record, false)
}

// WriteDeterministic uses a stable partition filename and returns an existing
// identical record. It makes legacy migration resumable after an index failure.
func (s *Store) WriteDeterministic(record Record) (Result, error) {
	return s.write(record, true)
}

func (s *Store) write(record Record, deterministic bool) (Result, error) {
	if s == nil {
		return Result{}, errors.New("conversation archive store is nil")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if err := validateRecordIdentity(record); err != nil {
		return Result{}, err
	}
	var estimatedBytes int64
	for _, payload := range record.Payloads {
		estimatedBytes += int64(len(payload))
	}
	for _, value := range record.Metadata {
		estimatedBytes += int64(len(value))
	}
	if err := s.CheckWritable(estimatedBytes); err != nil {
		return Result{}, err
	}

	env := envelope{
		Version:           CurrentVersion,
		ID:                record.ID,
		RecordedAt:        record.RecordedAt,
		Protocol:          record.Protocol,
		PartitionTimezone: s.location.String(),
		Payloads:          make(map[string]storedValue, len(record.Payloads)),
		Metadata:          make(map[string]storedValue, len(record.Metadata)),
		Completeness:      record.Completeness,
	}

	var referencedBlobs = make(map[string]int64)
	for name, payload := range record.Payloads {
		if err := validateFieldName(name); err != nil {
			return Result{}, fmt.Errorf("invalid payload name: %w", err)
		}
		stored, err := s.storePayload(payload)
		if err != nil {
			return Result{}, fmt.Errorf("store payload %q: %w", name, err)
		}
		env.Payloads[name] = stored
		env.OriginalBytes += int64(len(payload))
		collectBlobSizes(stored, referencedBlobs)
	}
	for name, value := range record.Metadata {
		if err := validateFieldName(name); err != nil {
			return Result{}, fmt.Errorf("invalid metadata name: %w", err)
		}
		stored, err := s.storeBytes([]byte(value), false)
		if err != nil {
			return Result{}, fmt.Errorf("store metadata %q: %w", name, err)
		}
		env.Metadata[name] = stored
		env.OriginalBytes += int64(len(value))
		collectBlobSizes(stored, referencedBlobs)
	}
	env.ContentSHA256 = contentDigest(record.Payloads, record.Metadata)

	return s.writeEnvelope(record, env, referencedBlobs, deterministic)
}

func (s *Store) writeEnvelope(record Record, env envelope, referencedBlobs map[string]int64, deterministic bool) (Result, error) {
	plainEnvelope, err := common.Marshal(env)
	if err != nil {
		return Result{}, fmt.Errorf("marshal conversation archive envelope: %w", err)
	}
	compressedEnvelope, err := gzipBytes(plainEnvelope)
	if err != nil {
		return Result{}, fmt.Errorf("compress conversation archive envelope: %w", err)
	}

	partition := record.RecordedAt.In(s.location).Format("2006/01/02")
	recordDir := filepath.Join(s.root, "records", filepath.FromSlash(partition))
	if err := s.ensureDir(recordDir); err != nil {
		return Result{}, fmt.Errorf("create conversation archive partition: %w", err)
	}
	filename := record.ID + ".json.gz"
	if !deterministic {
		randomSuffix, err := randomRecordSuffix()
		if err != nil {
			return Result{}, fmt.Errorf("generate conversation archive filename: %w", err)
		}
		filename = record.ID + "-" + randomSuffix + ".json.gz"
	}
	absolutePath := filepath.Join(recordDir, filename)
	if deterministic {
		if _, err := os.Lstat(absolutePath); err == nil {
			relativePath, relErr := filepath.Rel(s.root, absolutePath)
			if relErr != nil {
				return Result{}, relErr
			}
			existing, existingResult, restoreErr := s.Restore(relativePath)
			if restoreErr != nil {
				return Result{}, fmt.Errorf("verify deterministic conversation archive record: %w", restoreErr)
			}
			if existing.ID != record.ID || !existing.RecordedAt.Equal(record.RecordedAt) || existing.Protocol != record.Protocol ||
				existing.Completeness.Complete != record.Completeness.Complete || !equalStrings(existing.Completeness.Missing, record.Completeness.Missing) ||
				contentDigest(existing.Payloads, existing.Metadata) != env.ContentSHA256 {
				return Result{}, fmt.Errorf("deterministic conversation archive record %s already exists with different content", record.ID)
			}
			return existingResult, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return Result{}, fmt.Errorf("inspect deterministic conversation archive record: %w", err)
		}
	}
	if err := s.writeAtomic(absolutePath, compressedEnvelope); err != nil {
		return Result{}, fmt.Errorf("write conversation archive record: %w", err)
	}

	relativePath, err := filepath.Rel(s.root, absolutePath)
	if err != nil {
		return Result{}, fmt.Errorf("resolve conversation archive record path: %w", err)
	}
	storedBytes := int64(len(compressedEnvelope))
	for _, size := range referencedBlobs {
		storedBytes += size
	}
	return Result{
		RecordPath:    filepath.ToSlash(relativePath),
		SHA256:        digestHex(compressedEnvelope),
		OriginalBytes: env.OriginalBytes,
		StoredBytes:   storedBytes,
		Integrity:     completenessStatus(record.Completeness),
	}, nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// Restore reads, verifies, and losslessly reconstructs a record. recordPath
// must be the relative path returned by Write.
func (s *Store) Restore(recordPath string) (Record, Result, error) {
	absolutePath, err := s.secureRecordPath(recordPath)
	if err != nil {
		return Record{}, Result{}, err
	}
	compressedEnvelope, err := os.ReadFile(absolutePath)
	if err != nil {
		return Record{}, Result{}, fmt.Errorf("read conversation archive record: %w", err)
	}
	plainEnvelope, err := gunzipBytes(compressedEnvelope)
	if err != nil {
		return Record{}, Result{}, fmt.Errorf("decompress conversation archive record: %w", err)
	}
	var env envelope
	if err := common.Unmarshal(plainEnvelope, &env); err != nil {
		return Record{}, Result{}, fmt.Errorf("decode conversation archive envelope: %w", err)
	}
	if env.Version != CurrentVersion {
		return Record{}, Result{}, fmt.Errorf("unsupported conversation archive envelope version %d", env.Version)
	}

	record := Record{
		ID:           env.ID,
		RecordedAt:   env.RecordedAt,
		Protocol:     env.Protocol,
		Payloads:     make(map[string][]byte, len(env.Payloads)),
		Metadata:     make(map[string]string, len(env.Metadata)),
		Completeness: env.Completeness,
	}
	var referencedBlobs = make(map[string]int64)
	var originalBytes int64
	for name, stored := range env.Payloads {
		value, err := s.restoreValue(stored)
		if err != nil {
			return Record{}, Result{}, fmt.Errorf("restore payload %q: %w", name, err)
		}
		record.Payloads[name] = value
		originalBytes += int64(len(value))
		collectBlobSizes(stored, referencedBlobs)
	}
	for name, stored := range env.Metadata {
		value, err := s.restoreValue(stored)
		if err != nil {
			return Record{}, Result{}, fmt.Errorf("restore metadata %q: %w", name, err)
		}
		record.Metadata[name] = string(value)
		originalBytes += int64(len(value))
		collectBlobSizes(stored, referencedBlobs)
	}
	if originalBytes != env.OriginalBytes {
		return Record{}, Result{}, fmt.Errorf("conversation archive size mismatch: got %d, want %d", originalBytes, env.OriginalBytes)
	}
	if digest := contentDigest(record.Payloads, record.Metadata); digest != env.ContentSHA256 {
		return Record{}, Result{}, errors.New("conversation archive content hash mismatch")
	}
	storedBytes := int64(len(compressedEnvelope))
	for _, size := range referencedBlobs {
		storedBytes += size
	}
	return record, Result{
		RecordPath:    filepath.ToSlash(filepath.Clean(recordPath)),
		SHA256:        digestHex(compressedEnvelope),
		OriginalBytes: originalBytes,
		StoredBytes:   storedBytes,
		Integrity:     completenessStatus(env.Completeness),
	}, nil
}

func (s *Store) storePayload(data []byte) (storedValue, error) {
	if !json.Valid(data) {
		return s.storeBytes(data, false)
	}
	ranges := s.largeJSONStringRanges(data)
	if len(ranges) == 0 {
		return s.storeBytes(data, true)
	}
	stored := storedValue{Size: int64(len(data)), SHA256: digestHex(data)}
	start := 0
	for _, current := range ranges {
		if current[0] > start {
			stored.Chunks = append(stored.Chunks, storedChunk{Kind: "inline", Inline: bytes.Clone(data[start:current[0]])})
		}
		ref, err := s.storeBlob(data[current[0]:current[1]])
		if err != nil {
			return storedValue{}, err
		}
		stored.Chunks = append(stored.Chunks, storedChunk{Kind: "blob", Blob: &ref})
		start = current[1]
	}
	if start < len(data) {
		stored.Chunks = append(stored.Chunks, storedChunk{Kind: "inline", Inline: bytes.Clone(data[start:])})
	}
	return stored, nil
}

func (s *Store) storeBytes(data []byte, keepInline bool) (storedValue, error) {
	stored := storedValue{Size: int64(len(data)), SHA256: digestHex(data)}
	if len(data) < s.blobThreshold || keepInline {
		stored.Chunks = []storedChunk{{Kind: "inline", Inline: bytes.Clone(data)}}
		return stored, nil
	}
	ref, err := s.storeBlob(data)
	if err != nil {
		return storedValue{}, err
	}
	stored.Chunks = []storedChunk{{Kind: "blob", Blob: &ref}}
	return stored, nil
}

func (s *Store) storeBlob(data []byte) (blobRef, error) {
	digest := digestHex(data)
	dir := filepath.Join(s.root, "blobs", "sha256", digest[:2])
	if err := s.ensureDir(dir); err != nil {
		return blobRef{}, fmt.Errorf("create conversation archive blob directory: %w", err)
	}
	path := filepath.Join(dir, digest+".gz")
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return blobRef{}, fmt.Errorf("conversation archive blob is not a regular file: %s", digest)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return blobRef{}, fmt.Errorf("secure existing conversation archive blob: %w", err)
		}
		if err := s.verifyBlob(path, digest, int64(len(data))); err != nil {
			return blobRef{}, err
		}
		return blobRef{SHA256: digest, Size: int64(len(data)), StoredSize: info.Size()}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return blobRef{}, fmt.Errorf("inspect conversation archive blob: %w", err)
	}

	compressed, err := gzipBytes(data)
	if err != nil {
		return blobRef{}, fmt.Errorf("compress conversation archive blob: %w", err)
	}
	if err := s.writeAtomic(path, compressed); err != nil {
		return blobRef{}, fmt.Errorf("write conversation archive blob: %w", err)
	}
	return blobRef{SHA256: digest, Size: int64(len(data)), StoredSize: int64(len(compressed))}, nil
}

func (s *Store) restoreValue(stored storedValue) ([]byte, error) {
	var restored bytes.Buffer
	for _, chunk := range stored.Chunks {
		switch {
		case chunk.Kind == "blob" && chunk.Blob != nil && chunk.Inline == nil:
			blob, err := s.readBlob(*chunk.Blob)
			if err != nil {
				return nil, err
			}
			_, _ = restored.Write(blob)
		case chunk.Kind == "inline" && chunk.Blob == nil:
			_, _ = restored.Write(chunk.Inline)
		default:
			return nil, errors.New("conversation archive chunk must contain exactly one storage form")
		}
	}
	value := restored.Bytes()
	if int64(len(value)) != stored.Size {
		return nil, fmt.Errorf("conversation archive value size mismatch: got %d, want %d", len(value), stored.Size)
	}
	if digestHex(value) != stored.SHA256 {
		return nil, errors.New("conversation archive value hash mismatch")
	}
	return bytes.Clone(value), nil
}

func (s *Store) readBlob(ref blobRef) ([]byte, error) {
	if !validDigest(ref.SHA256) || ref.Size < 0 || ref.StoredSize < 0 {
		return nil, errors.New("invalid conversation archive blob reference")
	}
	path := filepath.Join(s.root, "blobs", "sha256", ref.SHA256[:2], ref.SHA256+".gz")
	compressed, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read conversation archive blob %s: %w", ref.SHA256, err)
	}
	if int64(len(compressed)) != ref.StoredSize {
		return nil, fmt.Errorf("conversation archive blob %s stored size mismatch", ref.SHA256)
	}
	data, err := gunzipBytes(compressed)
	if err != nil {
		return nil, fmt.Errorf("decompress conversation archive blob %s: %w", ref.SHA256, err)
	}
	if int64(len(data)) != ref.Size || digestHex(data) != ref.SHA256 {
		return nil, fmt.Errorf("conversation archive blob %s failed integrity verification", ref.SHA256)
	}
	return data, nil
}

func (s *Store) verifyBlob(path, digest string, size int64) error {
	compressed, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("read existing conversation archive blob: %w", err)
	}
	defer compressed.Close()
	reader, err := gzip.NewReader(compressed)
	if err != nil {
		return fmt.Errorf("decompress existing conversation archive blob: %w", err)
	}
	defer reader.Close()
	hasher := sha256.New()
	actualSize, err := io.Copy(hasher, reader)
	if err != nil {
		return fmt.Errorf("read existing conversation archive blob: %w", err)
	}
	if actualSize != size || hex.EncodeToString(hasher.Sum(nil)) != digest {
		return fmt.Errorf("existing conversation archive blob %s failed integrity verification", digest)
	}
	return nil
}

func (s *Store) secureRecordPath(recordPath string) (string, error) {
	if recordPath == "" || filepath.IsAbs(recordPath) || !filepath.IsLocal(recordPath) || strings.Contains(recordPath, `\`) {
		return "", errors.New("conversation archive record path must be a safe relative path")
	}
	clean := filepath.Clean(recordPath)
	if !strings.HasPrefix(filepath.ToSlash(clean), "records/") || !strings.HasSuffix(clean, ".json.gz") {
		return "", errors.New("conversation archive record path is outside the records namespace")
	}
	absolutePath := filepath.Join(s.root, clean)
	resolvedPath, err := filepath.EvalSymlinks(absolutePath)
	if err != nil {
		return "", fmt.Errorf("resolve conversation archive record path: %w", err)
	}
	relative, err := filepath.Rel(s.root, resolvedPath)
	if err != nil || !filepath.IsLocal(relative) {
		return "", errors.New("conversation archive record path escapes archive root")
	}
	return resolvedPath, nil
}

func (s *Store) ensureDir(path string) error {
	relative, err := filepath.Rel(s.root, path)
	if err != nil || (!filepath.IsLocal(relative) && relative != ".") {
		return errors.New("conversation archive directory escapes archive root")
	}
	current := s.root
	parts := strings.Split(filepath.Clean(relative), string(filepath.Separator))
	if relative == "." {
		parts = nil
	}
	for _, part := range parts {
		current = filepath.Join(current, part)
		if err := os.Mkdir(current, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("conversation archive path component is not a directory: %s", current)
		}
		if err := os.Chmod(current, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) writeAtomic(path string, data []byte) (err error) {
	temp, err := os.CreateTemp(filepath.Dir(path), ".conversation-archive-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		if err != nil {
			_ = os.Remove(tempName)
		}
	}()
	if err = temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err = temp.Write(data); err != nil {
		return err
	}
	if err = temp.Sync(); err != nil {
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if err = s.rename(tempName, path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (s *Store) largeJSONStringRanges(data []byte) [][2]int {
	var ranges [][2]int
	for index := 0; index < len(data); index++ {
		if data[index] != '"' {
			continue
		}
		start := index
		index++
		for ; index < len(data); index++ {
			if data[index] == '\\' {
				index++
				continue
			}
			if data[index] != '"' {
				continue
			}
			var decoded string
			if err := common.Unmarshal(data[start:index+1], &decoded); err == nil && len(decoded) >= s.blobThreshold {
				ranges = append(ranges, [2]int{start, index + 1})
			}
			break
		}
	}
	return ranges
}

func validateFieldName(name string) error {
	if name == "" {
		return errors.New("field name is empty")
	}
	if strings.ContainsAny(name, "\x00\r\n") {
		return fmt.Errorf("field name %q contains control characters", name)
	}
	return nil
}

func validateRecordIdentity(record Record) error {
	if !recordIDPattern.MatchString(record.ID) || record.ID == "." || record.ID == ".." {
		return fmt.Errorf("invalid conversation archive record id %q", record.ID)
	}
	if record.RecordedAt.IsZero() {
		return errors.New("conversation archive recorded time is required")
	}
	if strings.TrimSpace(record.Protocol) == "" {
		return errors.New("conversation archive protocol is required")
	}
	return nil
}

func collectBlobSizes(stored storedValue, sizes map[string]int64) {
	for _, chunk := range stored.Chunks {
		if chunk.Blob != nil {
			sizes[chunk.Blob.SHA256] = chunk.Blob.StoredSize
		}
	}
}

func contentDigest(payloads map[string][]byte, metadata map[string]string) string {
	hasher := newContentHash()
	writeDomainMap := func(domain string, values map[string][]byte) {
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			writeContentHeader(hasher, domain, key, int64(len(values[key])))
			_, _ = hasher.Write(values[key])
		}
	}
	writeDomainMap("payload", payloads)
	metadataBytes := make(map[string][]byte, len(metadata))
	for key, value := range metadata {
		metadataBytes[key] = []byte(value)
	}
	writeDomainMap("metadata", metadataBytes)
	return hex.EncodeToString(hasher.Sum(nil))
}

func newContentHash() hash.Hash {
	return sha256.New()
}

func writeContentHeader(writer io.Writer, domain, key string, size int64) {
	_, _ = io.WriteString(writer, domain)
	_, _ = io.WriteString(writer, fmt.Sprintf("\x00%d\x00%s\x00%d\x00", len(key), key, size))
}

func completenessStatus(completeness Completeness) IntegrityStatus {
	if completeness.Complete && len(completeness.Missing) == 0 {
		return IntegrityComplete
	}
	return IntegrityIncomplete
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func digestHex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func gzipBytes(data []byte) ([]byte, error) {
	var output bytes.Buffer
	writer := gzip.NewWriter(&output)
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func gunzipBytes(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}
