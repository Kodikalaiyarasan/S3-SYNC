# s3-sync

**s3-sync** is a high-performance, concurrent Command-Line Interface (CLI) utility written in Go for mirroring local directories to S3-compatible cloud storage providers (AWS S3, MinIO, Cloudflare R2, Wasabi, DigitalOcean Spaces) and vice versa.

It utilizes parallel worker pools, smart delta detection via ETag/MD5 checksums, multi-source configuration via Viper, and bucket maintenance utilities.

---

## Features

- **S3 Storage Support**: Native AWS S3, Cloudflare R2, Wasabi, DigitalOcean Spaces.
- **Smart Delta Syncing**: Computes local MD5 hashes and compares with remote S3 ETags, file sizes, and timestamps to skip identical files.
- **High-Concurrency Worker Pool**: Configurable multi-threaded worker pool (`--concurrency`, default 10) for parallel file transfers.
- **Configuration Precedence**:
  1. CLI Flags (highest)
  2. Environment Variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`, `S3_ENDPOINT_URL`)
  3. Configuration file (`conf/config.yaml` or `~/.s3-sync.yaml`) (lowest)
- **Formatted ASCII Output**: Clear audit tables for `diff`, `ls`, and `quota` commands.

---

## Project Structure

```
s3-sync/
├── cmd/
│   ├── root.go       # Root Cobra command, global flags, and Viper setup
│   ├── push.go       # "push" subcommand (upload & mirror)
│   ├── pull.go       # "pull" subcommand (download & mirror)
│   ├── diff.go       # "diff" subcommand (hash audit & comparison)
│   ├── ls.go         # "ls" subcommand (multi-threaded explorer)
│   ├── quota.go      # "quota" subcommand (size & cost analyzer)
│   └── clean.go      # "clean" subcommand (orphan upload purge)
├── conf/
│   └── config.yaml   # Credentials and endpoint configuration
├── pkg/
│   ├── s3client/     # AWS SDK v2 client initialization and URI parsing
│   ├── sync/         # Core delta evaluation and worker pool engine
│   └── ui/           # Formatted ASCII tables and byte formatters
├── Dockerfile        # Multi-stage production container
├── go.mod            # Dependencies
└── main.go           # CLI entry point
```

---

## Configuration (`conf/config.yaml`)

Place your cloud storage credentials inside `conf/config.yaml`:

```yaml
# 1. AWS Credentials
access-key-id: "YOUR_AWS_ACCESS_KEY_ID"
secret-access-key: "YOUR_AWS_SECRET_ACCESS_KEY"
# session-token: "" # Optional for temporary STS credentials

# 2. Region (e.g. us-east-1, ap-south-2)
region: "ap-south-2"

# 3. Custom S3-Compatible Endpoint (Optional - MinIO, R2, Wasabi)
# endpoint-url: "http://localhost:9000"

# 4. Path-Style Addressing (Set true for MinIO/Ceph, false for AWS S3)
path-style: false
```

---

## Quickstart with Docker

### 1. Build the Docker Image
```powershell
docker build -t s3-sync:latest .
```

### 2. Run the Interactive Container
Mount your current directory so local files and your `conf/config.yaml` are immediately available:
```powershell
docker run -it --rm -v "${PWD}:/workspace" s3-sync:latest /bin/sh
```

---

## CLI Commands Reference

Inside the container or host shell:

### 1. `ls` — Rapid Multi-Threaded Explorer
List objects under a bucket and prefix:
```sh
s3-sync ls s3://my-bucket/
s3-sync ls s3://my-bucket/prefix/ --human-readable --recursive --storage-class
```

### 2. `diff` — Hash Comparison & Audit
Audit differences between local directory and S3 bucket without transferring data:
```sh
s3-sync diff ./sample s3://my-bucket/backup/
```
Outputs an ASCII table showing:
- `IN_SYNC`: Identical hashes and sizes (skipped during sync).
- `MISSING_IN_S3`: File exists locally, needs upload.
- `MODIFIED`: Local hash differs from remote ETag.
- `ORPHAN_IN_S3`: File exists in S3 but missing locally.

### 3. `push` — Upload & Mirror (Local ➔ S3)
Mirror local directory to S3 with delta skipping:
```sh
s3-sync push ./sample s3://my-bucket/backup/ --concurrency 10
```
Flags:
- `-c, --concurrency <int>`: Parallel upload threads (default: 10).
- `--dry-run`: Preview files to be uploaded without transferring.
- `--delete`: Purge remote objects that no longer exist locally.

### 4. `pull` — Download & Mirror (S3 ➔ Local)
Download missing or modified objects from S3 to local disk:
```sh
s3-sync pull s3://my-bucket/backup/ ./sample/ --concurrency 10
```
Flags:
- `-c, --concurrency <int>`: Parallel download threads (default: 10).
- `--dry-run`: Preview files to be downloaded without writing to disk.
- `--delete`: Remove local files if they do not exist in the remote prefix.

### 5. `quota` — Bucket Size & Cost Analyzer
Aggregate total object count, volume size, storage class distribution, top files, and estimated AWS monthly costs:
```sh
s3-sync quota s3://my-bucket/ --top-files 10
```

### 6. `clean` — Housekeeping & Incomplete Multipart Upload Purge
Find and abort incomplete multipart uploads older than 24 hours to eliminate hidden storage costs:
```sh
s3-sync clean s3://my-bucket/ --abort-multipart
```
Flags:
- `--abort-multipart`: Purge stale multipart uploads.
- `--dry-run`: Preview multipart uploads scheduled for abort.

---

## Custom Endpoints (Wasabi / Cloudflare R2 / Custom S3)

To sync with custom S3 providers, set `endpoint-url` in `conf/config.yaml` or use CLI flags:

```sh
s3-sync push ./sample s3://my-bucket/test \
  --endpoint-url=https://s3.us-west-004.backblazeb2.com
```
