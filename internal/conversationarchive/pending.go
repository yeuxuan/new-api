package conversationarchive

import (
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// PendingCapture incrementally spools one active conversation on the archive
// volume. Its descriptor and raw payload files intentionally remain under
// pending/ when finalization fails or the process exits unexpectedly.
type PendingCapture struct {
	mu       sync.Mutex
	store    *Store
	dir      string
	lockPath string
	lockFile *os.File
	record   Record
	files    map[string]*os.File
	paths    map[string]string
	sizes    map[string]int64
	excluded map[string]struct{}
	closed   bool
	writeErr error
}

type pendingDescriptor struct {
	Version      int               `json:"version"`
	State        string            `json:"state"`
	ID           string            `json:"id"`
	RecordedAt   time.Time         `json:"recorded_at"`
	Protocol     string            `json:"protocol"`
	PayloadFiles map[string]string `json:"payload_files"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	Completeness Completeness      `json:"completeness"`
	Excluded     []string          `json:"excluded_payloads,omitempty"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// RecoverPending converts interrupted staging directories into explicitly
// incomplete immutable records. No pending bytes are discarded before the
// recovered envelope is durable.
func (s *Store) RecoverPending() ([]Result, error) {
	paths, err := s.PendingPaths()
	if err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(paths))
	for _, relativeDir := range paths {
		dir := filepath.Join(s.root, filepath.FromSlash(relativeDir))
		lockPath := pendingLockPath(dir)
		lockFile, locked, err := acquirePendingLock(lockPath)
		if err != nil {
			return results, fmt.Errorf("lock pending capture %s: %w", relativeDir, err)
		}
		if !locked {
			return results, fmt.Errorf("pending capture is still active: %s", relativeDir)
		}
		info, statErr := os.Lstat(dir)
		if errors.Is(statErr, os.ErrNotExist) {
			_ = releasePendingLock(lockFile)
			_ = os.Remove(lockPath)
			continue
		}
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			_ = releasePendingLock(lockFile)
			if statErr != nil {
				return results, fmt.Errorf("reinspect pending capture %s: %w", relativeDir, statErr)
			}
			return results, fmt.Errorf("pending capture changed type while acquiring lock: %s", relativeDir)
		}
		result, retiredDir, err := s.recoverPendingLocked(relativeDir, dir)
		releaseErr := releasePendingLock(lockFile)
		if err != nil {
			return results, err
		}
		if releaseErr != nil {
			return results, fmt.Errorf("unlock recovered pending capture %s: %w", relativeDir, releaseErr)
		}
		if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return results, fmt.Errorf("remove recovered pending lock %s: %w", relativeDir, err)
		}
		if err := os.RemoveAll(retiredDir); err != nil {
			return results, fmt.Errorf("remove recovered pending capture %s: %w", relativeDir, err)
		}
		results = append(results, result)
	}
	return results, nil
}

func (s *Store) recoverPendingLocked(relativeDir, dir string) (Result, string, error) {
	descriptorData, err := os.ReadFile(filepath.Join(dir, "pending.json"))
	if err != nil {
		return Result{}, "", fmt.Errorf("read pending descriptor %s: %w", relativeDir, err)
	}
	var descriptor pendingDescriptor
	if err := common.Unmarshal(descriptorData, &descriptor); err != nil {
		return Result{}, "", fmt.Errorf("decode pending descriptor %s: %w", relativeDir, err)
	}
	if descriptor.Version != CurrentVersion {
		return Result{}, "", fmt.Errorf("unsupported pending descriptor version %d", descriptor.Version)
	}
	switch descriptor.State {
	case "capturing", "incomplete", "ready":
	default:
		return Result{}, "", fmt.Errorf("unsupported pending descriptor state %q", descriptor.State)
	}
	completeness := descriptor.Completeness
	if descriptor.State != "ready" {
		completeness = Completeness{
			Complete: false,
			Missing:  appendUnique(descriptor.Completeness.Missing, "recovered_after_interruption"),
		}
	}
	record := Record{
		ID:           descriptor.ID,
		RecordedAt:   descriptor.RecordedAt,
		Protocol:     descriptor.Protocol,
		Metadata:     descriptor.Metadata,
		Completeness: completeness,
	}
	if err := validateRecordIdentity(record); err != nil {
		return Result{}, "", fmt.Errorf("invalid pending descriptor %s: %w", relativeDir, err)
	}
	excluded := make(map[string]struct{}, len(descriptor.Excluded))
	for _, name := range descriptor.Excluded {
		if !strings.HasPrefix(name, "recovery_raw_") {
			return Result{}, "", fmt.Errorf("pending descriptor %s excludes non-recovery payload %q", relativeDir, name)
		}
		if _, exists := descriptor.PayloadFiles[name]; !exists {
			return Result{}, "", fmt.Errorf("pending descriptor %s excludes unknown payload %q", relativeDir, name)
		}
		excluded[name] = struct{}{}
	}
	payloadFiles := make(map[string]string, len(descriptor.PayloadFiles))
	for name, filename := range descriptor.PayloadFiles {
		if _, skip := excluded[name]; skip {
			continue
		}
		if err := validateFieldName(name); err != nil {
			return Result{}, "", err
		}
		if filepath.Base(filename) != filename || filename == "." || filename == ".." {
			return Result{}, "", fmt.Errorf("invalid pending payload filename %q", filename)
		}
		path := filepath.Join(dir, filename)
		info, err := os.Lstat(path)
		if err != nil {
			return Result{}, "", fmt.Errorf("inspect pending payload %q: %w", name, err)
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return Result{}, "", fmt.Errorf("pending payload %q is not a regular file", name)
		}
		payloadFiles[name] = path
	}
	result, found, err := s.findEquivalentRecord(record, payloadFiles)
	if err != nil {
		return Result{}, "", fmt.Errorf("match pending capture %s to immutable records: %w", relativeDir, err)
	}
	if !found {
		result, err = s.writeFileRecord(record, payloadFiles)
		if err != nil {
			return Result{}, "", fmt.Errorf("recover pending capture %s: %w", relativeDir, err)
		}
	}
	if err := s.checkMountSentinel(); err != nil {
		return Result{}, "", err
	}
	retiredDir, err := s.retirePendingDirectory(dir, "recovered")
	if err != nil {
		return Result{}, "", fmt.Errorf("retire recovered pending capture %s: %w", relativeDir, err)
	}
	return result, retiredDir, nil
}

func pendingLockPath(dir string) string {
	return filepath.Join(filepath.Dir(dir), "."+filepath.Base(dir)+".lock")
}

func (s *Store) retirePendingDirectory(dir, state string) (string, error) {
	pendingRoot := filepath.Join(s.root, "pending")
	relative, err := filepath.Rel(pendingRoot, dir)
	if err != nil || !filepath.IsLocal(relative) || filepath.Dir(relative) != "." || !strings.HasSuffix(relative, ".pending") {
		return "", errors.New("conversation archive pending directory escapes pending namespace")
	}
	suffix, err := randomRecordSuffix()
	if err != nil {
		return "", err
	}
	retiredDir := filepath.Join(pendingRoot, "."+relative+"."+state+"-"+suffix)
	if err := os.Rename(dir, retiredDir); err != nil {
		return "", err
	}
	directory, err := os.Open(pendingRoot)
	if err != nil {
		if rollbackErr := os.Rename(retiredDir, dir); rollbackErr != nil {
			return "", fmt.Errorf("open pending directory after rename: %w; rollback rename: %v", err, rollbackErr)
		}
		return "", err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		if rollbackErr := os.Rename(retiredDir, dir); rollbackErr != nil {
			return "", fmt.Errorf("sync pending directory after rename: %w; rollback rename: %v", err, rollbackErr)
		}
		return "", err
	}
	return retiredDir, nil
}

func (s *Store) findEquivalentRecord(record Record, payloadFiles map[string]string) (Result, bool, error) {
	digest, err := contentDigestFiles(payloadFiles, record.Metadata)
	if err != nil {
		return Result{}, false, err
	}
	partition := record.RecordedAt.In(s.location).Format("2006/01/02")
	pattern := filepath.Join(s.root, "records", filepath.FromSlash(partition), record.ID+"*.json.gz")
	candidates, err := filepath.Glob(pattern)
	if err != nil {
		return Result{}, false, err
	}
	for _, candidate := range candidates {
		relative, err := filepath.Rel(s.root, candidate)
		if err != nil {
			return Result{}, false, err
		}
		existing, result, err := s.Restore(relative)
		if err != nil {
			return Result{}, false, err
		}
		if existing.ID != record.ID {
			continue
		}
		if !existing.RecordedAt.Equal(record.RecordedAt) || existing.Protocol != record.Protocol ||
			existing.Completeness.Complete != record.Completeness.Complete || !equalStrings(existing.Completeness.Missing, record.Completeness.Missing) ||
			contentDigest(existing.Payloads, existing.Metadata) != digest {
			return Result{}, false, fmt.Errorf("record id %s already exists with different content", record.ID)
		}
		return result, true, nil
	}
	return Result{}, false, nil
}

func appendUnique(values []string, value string) []string {
	result := append([]string(nil), values...)
	for _, existing := range result {
		if existing == value {
			return result
		}
	}
	return append(result, value)
}

// BeginPending creates the durable staging directory before any conversation
// bytes are accepted.
func (s *Store) BeginPending(record Record) (*PendingCapture, error) {
	if s == nil {
		return nil, errors.New("conversation archive store is nil")
	}
	if err := validateRecordIdentity(record); err != nil {
		return nil, err
	}
	if err := s.CheckWritable(0); err != nil {
		return nil, err
	}
	pendingRoot := filepath.Join(s.root, "pending")
	if err := s.ensureDir(pendingRoot); err != nil {
		return nil, fmt.Errorf("create conversation archive pending directory: %w", err)
	}
	dir, err := os.MkdirTemp(pendingRoot, record.ID+"-*.pending")
	if err != nil {
		return nil, fmt.Errorf("create conversation archive pending capture: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.Remove(dir)
		return nil, fmt.Errorf("secure conversation archive pending capture: %w", err)
	}
	lockPath := pendingLockPath(dir)
	lockFile, locked, err := acquirePendingLock(lockPath)
	if err != nil || !locked {
		_ = os.RemoveAll(dir)
		if err != nil {
			return nil, fmt.Errorf("lock conversation archive pending capture: %w", err)
		}
		return nil, errors.New("new conversation archive pending capture is unexpectedly locked")
	}
	pending := &PendingCapture{
		store:    s,
		dir:      dir,
		lockPath: lockPath,
		lockFile: lockFile,
		record:   record,
		files:    make(map[string]*os.File),
		paths:    make(map[string]string),
		sizes:    make(map[string]int64),
		excluded: make(map[string]struct{}),
	}
	if err := pending.writeDescriptorLocked("capturing", record.Metadata, Completeness{
		Complete: false,
		Missing:  []string{"capture_not_finalized"},
	}); err != nil {
		_ = releasePendingLock(lockFile)
		_ = os.Remove(lockPath)
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return pending, nil
}

// Append writes bytes directly to the archive volume instead of retaining the
// full stream in process memory.
func (p *PendingCapture) Append(name string, data []byte) error {
	if p == nil {
		return nil
	}
	if err := validateFieldName(name); err != nil {
		return err
	}
	if err := p.store.CheckWritable(int64(len(data))); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errors.New("conversation archive pending capture is closed")
	}
	if p.writeErr != nil {
		return p.writeErr
	}
	file := p.files[name]
	if file == nil {
		filename := fieldFilename(name)
		path := filepath.Join(p.dir, filename)
		var err error
		file, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			p.writeErr = fmt.Errorf("create pending payload %q: %w", name, err)
			return p.writeErr
		}
		p.files[name] = file
		p.paths[name] = path
		if err := p.writeDescriptorLocked("capturing", p.record.Metadata, Completeness{Complete: false, Missing: []string{"capture_not_finalized"}}); err != nil {
			p.writeErr = err
			return err
		}
	}
	n, err := file.Write(data)
	p.sizes[name] += int64(n)
	if err != nil {
		p.writeErr = fmt.Errorf("write pending payload %q: %w", name, err)
		return p.writeErr
	}
	if n != len(data) {
		p.writeErr = io.ErrShortWrite
		return p.writeErr
	}
	return nil
}

func (p *PendingCapture) Size(name string) int64 {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sizes[name]
}

// ExcludeFromRecord keeps recovery-only files in pending until the immutable
// record is durable, but omits them from the normal finalized envelope.
func (p *PendingCapture) ExcludeFromRecord(names ...string) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errors.New("conversation archive pending capture is closed")
	}
	for _, name := range names {
		if !strings.HasPrefix(name, "recovery_raw_") {
			return fmt.Errorf("only recovery_raw_ payloads may be excluded, got %q", name)
		}
		if _, exists := p.paths[name]; !exists {
			return fmt.Errorf("cannot exclude unknown recovery payload %q", name)
		}
		p.excluded[name] = struct{}{}
	}
	// Exclusions become durable only in Finalize after every payload file has
	// been synced. If the process exits before then, the older descriptor keeps
	// the raw recovery copies visible instead of risking a truncated tail.
	return nil
}

// Preserve closes an unsuccessful capture without deleting it. The raw files
// and descriptor remain available for inspection or later recovery.
func (p *PendingCapture) Preserve(metadata map[string]string, missing []string) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return p.writeErr
	}
	p.closed = true
	originalErr := p.writeErr
	for name, file := range p.files {
		if err := file.Sync(); err != nil && originalErr == nil {
			originalErr = fmt.Errorf("sync pending payload %q: %w", name, err)
		}
		if err := file.Close(); err != nil && originalErr == nil {
			originalErr = fmt.Errorf("close pending payload %q: %w", name, err)
		}
	}
	descriptorErr := p.writeDescriptorLocked("incomplete", metadata, Completeness{Complete: false, Missing: missing})
	lockErr := releasePendingLock(p.lockFile)
	p.lockFile = nil
	if originalErr != nil {
		return originalErr
	}
	if descriptorErr != nil {
		return descriptorErr
	}
	return lockErr
}

// Finalize syncs all pending payloads, writes the immutable envelope, and only
// then removes the staging directory. Any failure leaves recoverable raw files.
func (p *PendingCapture) Finalize(record Record) (Result, error) {
	if p == nil {
		return Result{}, errors.New("conversation archive pending capture is nil")
	}
	if err := validateRecordIdentity(record); err != nil {
		return Result{}, err
	}
	if record.ID != p.record.ID || !record.RecordedAt.Equal(p.record.RecordedAt) || record.Protocol != p.record.Protocol {
		return Result{}, errors.New("conversation archive pending identity changed during capture")
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return Result{}, errors.New("conversation archive pending capture is already closed")
	}
	p.closed = true
	for name, file := range p.files {
		if err := file.Sync(); err != nil && p.writeErr == nil {
			p.writeErr = fmt.Errorf("sync pending payload %q: %w", name, err)
		}
		if err := file.Close(); err != nil && p.writeErr == nil {
			p.writeErr = fmt.Errorf("close pending payload %q: %w", name, err)
		}
	}
	if p.writeErr == nil {
		p.writeErr = p.writeDescriptorLocked("ready", record.Metadata, record.Completeness)
	}
	writeErr := p.writeErr
	paths := make(map[string]string, len(p.paths))
	for name, path := range p.paths {
		if _, excluded := p.excluded[name]; excluded {
			continue
		}
		paths[name] = path
	}
	p.mu.Unlock()
	if writeErr != nil {
		_ = p.releaseLock()
		return Result{}, writeErr
	}

	result, err := p.store.writeFileRecord(record, paths)
	if err != nil {
		_ = p.releaseLock()
		return Result{}, err
	}
	if err := p.store.checkMountSentinel(); err != nil {
		_ = p.releaseLock()
		return result, err
	}
	retiredDir, err := p.store.retirePendingDirectory(p.dir, "finalized")
	if err != nil {
		_ = p.releaseLock()
		return result, fmt.Errorf("retire finalized conversation archive pending capture: %w", err)
	}
	if err := p.releaseLock(); err != nil {
		return result, fmt.Errorf("unlock finalized conversation archive pending capture: %w", err)
	}
	if err := os.Remove(p.lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, fmt.Errorf("remove finalized conversation archive pending lock: %w", err)
	}
	if err := os.RemoveAll(retiredDir); err != nil {
		return result, fmt.Errorf("remove finalized conversation archive pending capture: %w", err)
	}
	return result, nil
}

func (p *PendingCapture) releaseLock() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	err := releasePendingLock(p.lockFile)
	p.lockFile = nil
	return err
}

func (p *PendingCapture) writeDescriptorLocked(state string, metadata map[string]string, completeness Completeness) error {
	payloadFiles := make(map[string]string, len(p.paths))
	for name, path := range p.paths {
		payloadFiles[name] = filepath.Base(path)
	}
	var excluded []string
	if state == "ready" || state == "incomplete" {
		excluded = make([]string, 0, len(p.excluded))
		for name := range p.excluded {
			excluded = append(excluded, name)
		}
		sort.Strings(excluded)
	}
	descriptor, err := common.Marshal(pendingDescriptor{
		Version:      CurrentVersion,
		State:        state,
		ID:           p.record.ID,
		RecordedAt:   p.record.RecordedAt,
		Protocol:     p.record.Protocol,
		PayloadFiles: payloadFiles,
		Metadata:     metadata,
		Completeness: completeness,
		Excluded:     excluded,
		UpdatedAt:    time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("encode conversation archive pending descriptor: %w", err)
	}
	if err := p.store.writeAtomic(filepath.Join(p.dir, "pending.json"), descriptor); err != nil {
		return fmt.Errorf("write conversation archive pending descriptor: %w", err)
	}
	return nil
}

func (s *Store) writeFileRecord(record Record, payloadFiles map[string]string) (Result, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := validateRecordIdentity(record); err != nil {
		return Result{}, err
	}
	var estimated int64
	for _, path := range payloadFiles {
		info, err := os.Stat(path)
		if err != nil {
			return Result{}, fmt.Errorf("inspect pending payload: %w", err)
		}
		estimated += info.Size()
	}
	for _, value := range record.Metadata {
		estimated += int64(len(value))
	}
	if err := s.CheckWritable(estimated); err != nil {
		return Result{}, err
	}

	env := envelope{
		Version:           CurrentVersion,
		ID:                record.ID,
		RecordedAt:        record.RecordedAt,
		Protocol:          record.Protocol,
		PartitionTimezone: s.location.String(),
		Payloads:          make(map[string]storedValue, len(payloadFiles)),
		Metadata:          make(map[string]storedValue, len(record.Metadata)),
		Completeness:      record.Completeness,
	}
	referencedBlobs := make(map[string]int64)
	for name, path := range payloadFiles {
		if err := validateFieldName(name); err != nil {
			return Result{}, fmt.Errorf("invalid payload name: %w", err)
		}
		stored, err := s.storeFile(path)
		if err != nil {
			return Result{}, fmt.Errorf("store payload %q: %w", name, err)
		}
		env.Payloads[name] = stored
		env.OriginalBytes += stored.Size
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
	digest, err := contentDigestFiles(payloadFiles, record.Metadata)
	if err != nil {
		return Result{}, err
	}
	env.ContentSHA256 = digest
	return s.writeEnvelope(record, env, referencedBlobs, false)
}

func (s *Store) storeFile(path string) (storedValue, error) {
	info, err := os.Stat(path)
	if err != nil {
		return storedValue{}, err
	}
	if !info.Mode().IsRegular() {
		return storedValue{}, errors.New("pending payload is not a regular file")
	}
	if info.Size() < int64(s.blobThreshold) {
		data, err := os.ReadFile(path)
		if err != nil {
			return storedValue{}, err
		}
		return s.storeBytes(data, false)
	}
	ref, err := s.storeBlobFile(path)
	if err != nil {
		return storedValue{}, err
	}
	return storedValue{
		Size:   ref.Size,
		SHA256: ref.SHA256,
		Chunks: []storedChunk{{Kind: "blob", Blob: &ref}},
	}, nil
}

func (s *Store) storeBlobFile(sourcePath string) (returnRef blobRef, returnErr error) {
	digest, size, err := digestFile(sourcePath)
	if err != nil {
		return blobRef{}, err
	}
	dir := filepath.Join(s.root, "blobs", "sha256", digest[:2])
	if err := s.ensureDir(dir); err != nil {
		return blobRef{}, err
	}
	destination := filepath.Join(dir, digest+".gz")
	if info, err := os.Lstat(destination); err == nil {
		if !info.Mode().IsRegular() {
			return blobRef{}, fmt.Errorf("conversation archive blob is not a regular file: %s", digest)
		}
		if err := s.verifyBlob(destination, digest, size); err != nil {
			return blobRef{}, err
		}
		return blobRef{SHA256: digest, Size: size, StoredSize: info.Size()}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return blobRef{}, err
	}

	source, err := os.Open(sourcePath)
	if err != nil {
		return blobRef{}, err
	}
	defer source.Close()
	temp, err := os.CreateTemp(dir, ".conversation-blob-*.tmp")
	if err != nil {
		return blobRef{}, err
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		if returnErr != nil {
			_ = os.Remove(tempName)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return blobRef{}, err
	}
	compressor := gzip.NewWriter(temp)
	if _, err := io.Copy(compressor, source); err != nil {
		_ = compressor.Close()
		return blobRef{}, err
	}
	if err := compressor.Close(); err != nil {
		return blobRef{}, err
	}
	if err := temp.Sync(); err != nil {
		return blobRef{}, err
	}
	if err := temp.Close(); err != nil {
		return blobRef{}, err
	}
	info, err := os.Stat(tempName)
	if err != nil {
		return blobRef{}, err
	}
	if err := s.rename(tempName, destination); err != nil {
		return blobRef{}, err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return blobRef{}, err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return blobRef{}, err
	}
	return blobRef{SHA256: digest, Size: size, StoredSize: info.Size()}, nil
}

func contentDigestFiles(payloadFiles map[string]string, metadata map[string]string) (string, error) {
	hash := newContentHash()
	keys := make([]string, 0, len(payloadFiles))
	for key := range payloadFiles {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		file, err := os.Open(payloadFiles[key])
		if err != nil {
			return "", err
		}
		info, err := file.Stat()
		if err != nil {
			_ = file.Close()
			return "", err
		}
		writeContentHeader(hash, "payload", key, info.Size())
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	metadataKeys := make([]string, 0, len(metadata))
	for key := range metadata {
		metadataKeys = append(metadataKeys, key)
	}
	sort.Strings(metadataKeys)
	for _, key := range metadataKeys {
		value := metadata[key]
		writeContentHeader(hash, "metadata", key, int64(len(value)))
		_, _ = io.WriteString(hash, value)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func digestFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := newContentHash()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func fieldFilename(name string) string {
	hash := newContentHash()
	_, _ = io.WriteString(hash, name)
	return hex.EncodeToString(hash.Sum(nil)) + ".payload"
}

func randomRecordSuffix() (string, error) {
	value := make([]byte, 8)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
