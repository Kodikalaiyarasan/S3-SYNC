package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"

	"s3-sync/pkg/s3client"
	"s3-sync/pkg/ui"
)

var (
	cleanAbortMultipart bool
	cleanDryRun         bool
)

var cleanCmd = &cobra.Command{
	Use:   "clean s3://<bucket-name>/<prefix>",
	Short: "Housekeeping and orphaned upload purge",
	Long:  `Cleans up orphaned or incomplete multipart uploads older than 24 hours to recover storage costs.`,
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

		if !cleanAbortMultipart {
			fmt.Println("No housekeeping action specified. Use --abort-multipart to cancel incomplete multipart uploads.")
			return nil
		}

		fmt.Printf("Searching for incomplete multipart uploads in s3://%s/%s (age > 24 hours)...\n", bucket, prefix)

		input := &s3.ListMultipartUploadsInput{
			Bucket: aws.String(bucket),
		}
		if cleanPrefix != "" {
			input.Prefix = aws.String(cleanPrefix)
		}

		cutoff := time.Now().Add(-24 * time.Hour)
		var abortedCount int
		var candidateRows [][]string

		paginator := s3.NewListMultipartUploadsPaginator(client, input)
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return fmt.Errorf("failed listing multipart uploads: %w", err)
			}

			for _, upload := range page.Uploads {
				initiated := *upload.Initiated
				if initiated.Before(cutoff) {
					candidateRows = append(candidateRows, []string{
						aws.ToString(upload.Key),
						aws.ToString(upload.UploadId),
						initiated.Format("2006-01-02 15:04:05"),
					})

					if cleanDryRun {
						fmt.Printf("[DRY-RUN] Would abort upload for key: %s (ID: %s, Initiated: %s)\n",
							aws.ToString(upload.Key), aws.ToString(upload.UploadId), initiated)
					} else {
						_, abortErr := client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
							Bucket:   aws.String(bucket),
							Key:      upload.Key,
							UploadId: upload.UploadId,
						})
						if abortErr != nil {
							fmt.Printf("[ERROR] Failed to abort multipart upload for key %s (ID: %s): %v\n",
								aws.ToString(upload.Key), aws.ToString(upload.UploadId), abortErr)
						} else {
							fmt.Printf("[ABORTED] Multipart upload cancelled for key: %s (ID: %s)\n",
								aws.ToString(upload.Key), aws.ToString(upload.UploadId))
							abortedCount++
						}
					}
				}
			}
		}

		if len(candidateRows) > 0 {
			headers := []string{"KEY", "UPLOAD ID", "INITIATED AT"}
			fmt.Println("\n[Multipart Uploads Older Than 24h]")
			ui.RenderTable(nil, headers, candidateRows)
		} else {
			fmt.Println("No stale multipart uploads found.")
		}

		if cleanDryRun {
			fmt.Printf("\n[DRY-RUN] Total stale multipart uploads discovered: %d\n", len(candidateRows))
		} else {
			fmt.Printf("\nSuccessfully purged %d incomplete multipart uploads.\n", abortedCount)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(cleanCmd)

	cleanCmd.Flags().BoolVar(&cleanAbortMultipart, "abort-multipart", false, "Find and cancel incomplete multipart uploads older than 24 hours")
	cleanCmd.Flags().BoolVar(&cleanDryRun, "dry-run", false, "Preview objects or multipart uploads scheduled for deletion")
}
