package sync

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"strings"
)

// CalculateLocalMD5 calculates the standard MD5 checksum of a local file in lowercase hex.
func CalculateLocalMD5(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := md5.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// CleanETag strips quotes and potential whitespace from S3 ETag
func CleanETag(etag string) string {
	cleaned := strings.TrimSpace(etag)
	cleaned = strings.Trim(cleaned, "\"")
	return strings.ToLower(cleaned)
}

// IsETagMatch checks if a clean local MD5 matches the remote S3 ETag.
// Note: S3 ETags for single-part uploads are the MD5 hex string.
// Multipart uploads have format `<hash>-<part_count>`.
func IsETagMatch(localMD5, remoteETag string) bool {
	cleanRemote := CleanETag(remoteETag)
	if strings.Contains(cleanRemote, "-") {
		// Multipart upload ETag cannot be compared directly with simple MD5
		return false
	}
	return strings.EqualFold(localMD5, cleanRemote)
}
