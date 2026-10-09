package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"

	"s3-sync/pkg/s3client"
	"s3-sync/pkg/ui"
)

var (
	lsHumanReadable bool
	lsRecursive     bool
	lsStorageClass  bool
)

var lsCmd = &cobra.Command{
	Use:   "ls s3://<bucket-name>/<prefix>",
	Short: "Rapid multi-threaded explorer for S3 buckets",
	Long:  `Lists objects and directories under an S3 bucket and prefix. Supports recursive search and human-readable sizes.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		s3URI := args[0]
		bucket, prefix, err := s3client.ParseS3URI(s3URI)
		if err != nil {
			return err
		}

		ctx := context.Background()
		client, err := GetS3Client(ctx)
		if err != nil {
			return fmt.Errorf("failed to initialize S3 client: %w", err)
		}

		cleanPrefix := strings.TrimPrefix(prefix, "/")
		input := &s3.ListObjectsV2Input{
			Bucket: aws.String(bucket),
		}
		if cleanPrefix != "" {
			input.Prefix = aws.String(cleanPrefix)
		}
		if !lsRecursive {
			input.Delimiter = aws.String("/")
		}

		headers := []string{"LAST MODIFIED", "SIZE", "KEY"}
		if lsStorageClass {
			headers = append(headers, "STORAGE CLASS")
		}

		var rows [][]string
		var totalCount int64
		var totalBytes int64

		paginator := s3.NewListObjectsV2Paginator(client, input)
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return fmt.Errorf("error listing objects: %w", err)
			}

			// In non-recursive mode, show CommonPrefixes (folders)
			if !lsRecursive {
				for _, cp := range page.CommonPrefixes {
					dirKey := aws.ToString(cp.Prefix)
					row := []string{"PRE", "-", dirKey}
					if lsStorageClass {
						row = append(row, "-")
					}
					rows = append(rows, row)
				}
			}

			for _, item := range page.Contents {
				totalCount++
				size := aws.ToInt64(item.Size)
				totalBytes += size

				var sizeStr string
				if lsHumanReadable {
					sizeStr = ui.FormatBytes(size)
				} else {
					sizeStr = fmt.Sprintf("%d", size)
				}

				var modTimeStr string
				if item.LastModified != nil {
					modTimeStr = item.LastModified.Format("2006-01-02 15:04:05")
				} else {
					modTimeStr = "-"
				}

				row := []string{modTimeStr, sizeStr, aws.ToString(item.Key)}
				if lsStorageClass {
					row = append(row, string(item.StorageClass))
				}
				rows = append(rows, row)
			}
		}

		fmt.Printf("Listing s3://%s/%s\n", bucket, prefix)
		ui.RenderTable(nil, headers, rows)

		var totalSizeFormatted string
		if lsHumanReadable {
			totalSizeFormatted = ui.FormatBytes(totalBytes)
		} else {
			totalSizeFormatted = fmt.Sprintf("%d bytes", totalBytes)
		}
		fmt.Printf("\nTotal Objects: %d | Total Size: %s\n", totalCount, totalSizeFormatted)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(lsCmd)

	lsCmd.Flags().BoolVarP(&lsHumanReadable, "human-readable", "H", false, "Format file sizes into KB, MB, GB")
	lsCmd.Flags().BoolVar(&lsRecursive, "recursive", false, "Recursively traverse sub-prefixes")
	lsCmd.Flags().BoolVar(&lsStorageClass, "storage-class", false, "Show storage tier (STANDARD, INTELLIGENT_TIERING, GLACIER)")
}
