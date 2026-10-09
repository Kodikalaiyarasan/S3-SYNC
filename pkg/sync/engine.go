package sync

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type LocalFile struct {
	RelPath  string
	FullPath string
	Size     int64
	ModTime  time.Time
	MD5      string
}

type RemoteObject struct {
	Key          string
	RelPath      string
	Size         int64
	ETag         string
	LastModified time.Time
	StorageClass s3types.ObjectStorageClass
}

type DiffStatus string

const (
	StatusInSync      DiffStatus = "IN_SYNC"
	StatusMissingInS3 DiffStatus = "MISSING_IN_S3"
	StatusModified    DiffStatus = "MODIFIED"
	StatusOrphanInS3  DiffStatus = "ORPHAN_IN_S3"
)

type DiffItem struct {
	RelPath    string
	LocalSize  int64
	RemoteSize int64
	LocalMD5   string
	RemoteETag string
	Status     DiffStatus
	Reason     string
}

type Engine struct {
	client      *s3.Client
	concurrency int
}

func NewEngine(client *s3.Client, concurrency int) *Engine {
	if concurrency <= 0 {
		concurrency = 10
	}
	return &Engine{
		client:      client,
		concurrency: concurrency,
	}
}

// ScanLocalDir recursively scans a local directory and computes local file metadata (with MD5 hashes)
func ScanLocalDir(dirPath string) (map[string]LocalFile, error) {
	manifest := make(map[string]LocalFile)
	absDir, err := filepath.Abs(dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path of %s: %w", dirPath, err)
	}

	info, err := os.Stat(absDir)
	if err != nil {
		if os.IsNotExist(err) {
			return manifest, nil
		}
		return nil, fmt.Errorf("failed to stat directory %s: %w", absDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path %s is not a directory", absDir)
	}

	err = filepath.Walk(absDir, func(path string, fileInfo os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fileInfo.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(absDir, path)
		if err != nil {
			return err
		}
		// Standardize to forward slashes for S3 key consistency
		normalizedRel := filepath.ToSlash(rel)

		hash, err := CalculateLocalMD5(path)
		if err != nil {
			return fmt.Errorf("failed calculating md5 for %s: %w", path, err)
		}

		manifest[normalizedRel] = LocalFile{
			RelPath:  normalizedRel,
			FullPath: path,
			Size:     fileInfo.Size(),
			ModTime:  fileInfo.ModTime(),
			MD5:      hash,
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return manifest, nil
}

// ListRemoteObjects lists all objects under a bucket and prefix
func (e *Engine) ListRemoteObjects(ctx context.Context, bucket, prefix string) (map[string]RemoteObject, error) {
	manifest := make(map[string]RemoteObject)
	prefix = strings.TrimPrefix(prefix, "/")

	paginator := s3.NewListObjectsV2Paginator(e.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("error listing S3 objects for s3://%s/%s: %w", bucket, prefix, err)
		}

		for _, item := range page.Contents {
			key := aws.ToString(item.Key)
			rel := key
			if prefix != "" {
				if strings.HasPrefix(key, prefix) {
					rel = strings.TrimPrefix(key, prefix)
					rel = strings.TrimPrefix(rel, "/")
				}
			}

			// Skip directory placeholder keys
			if rel == "" || strings.HasSuffix(key, "/") {
				continue
			}

			var modTime time.Time
			if item.LastModified != nil {
				modTime = *item.LastModified
			}

			manifest[rel] = RemoteObject{
				Key:          key,
				RelPath:      rel,
				Size:         aws.ToInt64(item.Size),
				ETag:         CleanETag(aws.ToString(item.ETag)),
				LastModified: modTime,
				StorageClass: item.StorageClass,
			}
		}
	}

	return manifest, nil
}

// EvaluateDiff compares local files against remote objects to produce diff items
func EvaluateDiff(localFiles map[string]LocalFile, remoteFiles map[string]RemoteObject) []DiffItem {
	var diffs []DiffItem

	// Check all local files
	for rel, local := range localFiles {
		remote, exists := remoteFiles[rel]
		if !exists {
			diffs = append(diffs, DiffItem{
				RelPath:    rel,
				LocalSize:  local.Size,
				RemoteSize: 0,
				LocalMD5:   local.MD5,
				RemoteETag: "",
				Status:     StatusMissingInS3,
				Reason:     "File missing in remote S3 bucket",
			})
			continue
		}

		// File exists in both. Compare sizes and hashes
		if local.Size != remote.Size {
			diffs = append(diffs, DiffItem{
				RelPath:    rel,
				LocalSize:  local.Size,
				RemoteSize: remote.Size,
				LocalMD5:   local.MD5,
				RemoteETag: remote.ETag,
				Status:     StatusModified,
				Reason:     "File size mismatch",
			})
		} else if !IsETagMatch(local.MD5, remote.ETag) {
			diffs = append(diffs, DiffItem{
				RelPath:    rel,
				LocalSize:  local.Size,
				RemoteSize: remote.Size,
				LocalMD5:   local.MD5,
				RemoteETag: remote.ETag,
				Status:     StatusModified,
				Reason:     "MD5/ETag mismatch",
			})
		} else {
			diffs = append(diffs, DiffItem{
				RelPath:    rel,
				LocalSize:  local.Size,
				RemoteSize: remote.Size,
				LocalMD5:   local.MD5,
				RemoteETag: remote.ETag,
				Status:     StatusInSync,
				Reason:     "Hashes and sizes match",
			})
		}
	}

	// Check orphans (files that exist in remote but not locally)
	for rel, remote := range remoteFiles {
		if _, exists := localFiles[rel]; !exists {
			diffs = append(diffs, DiffItem{
				RelPath:    rel,
				LocalSize:  0,
				RemoteSize: remote.Size,
				LocalMD5:   "",
				RemoteETag: remote.ETag,
				Status:     StatusOrphanInS3,
				Reason:     "Object exists only in remote S3 bucket",
			})
		}
	}

	return diffs
}

type PushOptions struct {
	Bucket   string
	Prefix   string
	LocalDir string
	DryRun   bool
	Delete   bool
}

type SyncSummary struct {
	TransferredFiles int64
	TransferredBytes int64
	SkippedFiles     int64
	DeletedFiles     int64
	Errors           []error
}

// Push executes upload & mirror from local to S3
func (e *Engine) Push(ctx context.Context, opts PushOptions) (*SyncSummary, error) {
	summary := &SyncSummary{}

	localManifest, err := ScanLocalDir(opts.LocalDir)
	if err != nil {
		return nil, fmt.Errorf("local scan failed: %w", err)
	}

	remoteManifest, err := e.ListRemoteObjects(ctx, opts.Bucket, opts.Prefix)
	if err != nil {
		return nil, fmt.Errorf("remote scan failed: %w", err)
	}

	diffs := EvaluateDiff(localManifest, remoteManifest)

	var toUpload []LocalFile
	var toDelete []string

	for _, item := range diffs {
		switch item.Status {
		case StatusInSync:
			summary.SkippedFiles++
		case StatusMissingInS3, StatusModified:
			if lf, ok := localManifest[item.RelPath]; ok {
				toUpload = append(toUpload, lf)
			}
		case StatusOrphanInS3:
			if opts.Delete {
				if ro, ok := remoteManifest[item.RelPath]; ok {
					toDelete = append(toDelete, ro.Key)
				}
			}
		}
	}

	if opts.DryRun {
		for _, file := range toUpload {
			targetKey := buildRemoteKey(opts.Prefix, file.RelPath)
			fmt.Printf("[DRY-RUN] [PUSH] %s -> s3://%s/%s (%d bytes)\n", file.RelPath, opts.Bucket, targetKey, file.Size)
		}
		for _, key := range toDelete {
			fmt.Printf("[DRY-RUN] [DELETE] s3://%s/%s\n", opts.Bucket, key)
		}
		summary.TransferredFiles = int64(len(toUpload))
		summary.DeletedFiles = int64(len(toDelete))
		return summary, nil
	}

	// Concurrently upload files
	uploadChan := make(chan LocalFile, len(toUpload))
	for _, f := range toUpload {
		uploadChan <- f
	}
	close(uploadChan)

	var wg sync.WaitGroup
	var errLock sync.Mutex

	for i := 0; i < e.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range uploadChan {
				select {
				case <-ctx.Done():
					return
				default:
				}

				targetKey := buildRemoteKey(opts.Prefix, file.RelPath)
				fmt.Printf("[PUSH] Uploading %s -> s3://%s/%s ...\n", file.RelPath, opts.Bucket, targetKey)

				f, err := os.Open(file.FullPath)
				if err != nil {
					errLock.Lock()
					summary.Errors = append(summary.Errors, fmt.Errorf("open file %s: %w", file.FullPath, err))
					errLock.Unlock()
					continue
				}

				_, putErr := e.client.PutObject(ctx, &s3.PutObjectInput{
					Bucket: aws.String(opts.Bucket),
					Key:    aws.String(targetKey),
					Body:   f,
				})
				f.Close()

				if putErr != nil {
					errLock.Lock()
					summary.Errors = append(summary.Errors, fmt.Errorf("upload %s failed: %w", file.RelPath, putErr))
					errLock.Unlock()
				} else {
					atomic.AddInt64(&summary.TransferredFiles, 1)
					atomic.AddInt64(&summary.TransferredBytes, file.Size)
					fmt.Printf("[PUSH] Completed %s\n", file.RelPath)
				}
			}
		}()
	}

	wg.Wait()

	// Handle deletions if --delete is specified
	if opts.Delete && len(toDelete) > 0 {
		for _, key := range toDelete {
			fmt.Printf("[DELETE] s3://%s/%s\n", opts.Bucket, key)
			_, delErr := e.client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(opts.Bucket),
				Key:    aws.String(key),
			})
			if delErr != nil {
				summary.Errors = append(summary.Errors, fmt.Errorf("delete s3://%s/%s failed: %w", opts.Bucket, key, delErr))
			} else {
				summary.DeletedFiles++
			}
		}
	}

	return summary, nil
}

type PullOptions struct {
	Bucket   string
	Prefix   string
	LocalDir string
	DryRun   bool
	Delete   bool
}

// Pull executes download & mirror from S3 to local
func (e *Engine) Pull(ctx context.Context, opts PullOptions) (*SyncSummary, error) {
	summary := &SyncSummary{}

	localManifest, err := ScanLocalDir(opts.LocalDir)
	if err != nil {
		return nil, fmt.Errorf("local scan failed: %w", err)
	}

	remoteManifest, err := e.ListRemoteObjects(ctx, opts.Bucket, opts.Prefix)
	if err != nil {
		return nil, fmt.Errorf("remote scan failed: %w", err)
	}

	diffs := EvaluateDiff(localManifest, remoteManifest)

	var toDownload []RemoteObject
	var toDelete []LocalFile

	for _, item := range diffs {
		switch item.Status {
		case StatusInSync:
			summary.SkippedFiles++
		case StatusOrphanInS3, StatusModified:
			if ro, ok := remoteManifest[item.RelPath]; ok {
				toDownload = append(toDownload, ro)
			}
		case StatusMissingInS3:
			if opts.Delete {
				if lf, ok := localManifest[item.RelPath]; ok {
					toDelete = append(toDelete, lf)
				}
			}
		}
	}

	if opts.DryRun {
		for _, ro := range toDownload {
			destPath := filepath.Join(opts.LocalDir, filepath.FromSlash(ro.RelPath))
			fmt.Printf("[DRY-RUN] [PULL] s3://%s/%s -> %s (%d bytes)\n", opts.Bucket, ro.Key, destPath, ro.Size)
		}
		for _, lf := range toDelete {
			fmt.Printf("[DRY-RUN] [DELETE] %s\n", lf.FullPath)
		}
		summary.TransferredFiles = int64(len(toDownload))
		summary.DeletedFiles = int64(len(toDelete))
		return summary, nil
	}

	// Concurrently download files
	downloadChan := make(chan RemoteObject, len(toDownload))
	for _, ro := range toDownload {
		downloadChan <- ro
	}
	close(downloadChan)

	var wg sync.WaitGroup
	var errLock sync.Mutex

	for i := 0; i < e.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ro := range downloadChan {
				select {
				case <-ctx.Done():
					return
				default:
				}

				destPath := filepath.Join(opts.LocalDir, filepath.FromSlash(ro.RelPath))
				fmt.Printf("[PULL] Downloading s3://%s/%s -> %s ...\n", opts.Bucket, ro.Key, destPath)

				if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
					errLock.Lock()
					summary.Errors = append(summary.Errors, fmt.Errorf("mkdir %s: %w", filepath.Dir(destPath), err))
					errLock.Unlock()
					continue
				}

				getObj, err := e.client.GetObject(ctx, &s3.GetObjectInput{
					Bucket: aws.String(opts.Bucket),
					Key:    aws.String(ro.Key),
				})
				if err != nil {
					errLock.Lock()
					summary.Errors = append(summary.Errors, fmt.Errorf("get object s3://%s/%s: %w", opts.Bucket, ro.Key, err))
					errLock.Unlock()
					continue
				}

				outFile, err := os.Create(destPath)
				if err != nil {
					getObj.Body.Close()
					errLock.Lock()
					summary.Errors = append(summary.Errors, fmt.Errorf("create file %s: %w", destPath, err))
					errLock.Unlock()
					continue
				}

				written, err := io.Copy(outFile, getObj.Body)
				outFile.Close()
				getObj.Body.Close()

				if err != nil {
					errLock.Lock()
					summary.Errors = append(summary.Errors, fmt.Errorf("write file %s: %w", destPath, err))
					errLock.Unlock()
				} else {
					atomic.AddInt64(&summary.TransferredFiles, 1)
					atomic.AddInt64(&summary.TransferredBytes, written)
					fmt.Printf("[PULL] Completed %s\n", ro.RelPath)
				}
			}
		}()
	}

	wg.Wait()

	// Delete local files if --delete was passed
	if opts.Delete && len(toDelete) > 0 {
		for _, file := range toDelete {
			fmt.Printf("[DELETE] %s\n", file.FullPath)
			if err := os.Remove(file.FullPath); err != nil {
				summary.Errors = append(summary.Errors, fmt.Errorf("failed to remove local file %s: %w", file.FullPath, err))
			} else {
				summary.DeletedFiles++
			}
		}
	}

	return summary, nil
}

func buildRemoteKey(prefix, relPath string) string {
	cleanPrefix := strings.TrimPrefix(prefix, "/")
	cleanPrefix = strings.TrimSuffix(cleanPrefix, "/")
	cleanRel := strings.TrimPrefix(relPath, "/")

	if cleanPrefix == "" {
		return cleanRel
	}
	return cleanPrefix + "/" + cleanRel
}
