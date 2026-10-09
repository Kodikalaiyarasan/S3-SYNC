package s3client

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Config struct {
	Region          string
	EndpointURL     string
	UsePathStyle    bool
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// NewClient initializes an AWS SDK v2 S3 Client with custom endpoints and credentials
func NewClient(ctx context.Context, cfg Config) (*s3.Client, error) {
	var optFns []func(*awsconfig.LoadOptions) error

	if cfg.Region != "" {
		optFns = append(optFns, awsconfig.WithRegion(cfg.Region))
	} else {
		optFns = append(optFns, awsconfig.WithRegion("us-east-1"))
	}

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		optFns = append(optFns, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, optFns...)
	if err != nil {
		return nil, fmt.Errorf("unable to load AWS SDK config: %w", err)
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.UsePathStyle {
			o.UsePathStyle = true
		}
		if cfg.EndpointURL != "" {
			endpoint := cfg.EndpointURL
			if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
				endpoint = "http://" + endpoint
			}
			o.BaseEndpoint = aws.String(endpoint)
		}
	})

	return s3Client, nil
}

// ParseS3URI parses an s3://bucket/key URI into bucket and prefix/key
func ParseS3URI(rawURI string) (bucket string, prefix string, err error) {
	if !strings.HasPrefix(rawURI, "s3://") {
		return "", "", fmt.Errorf("invalid S3 URI %q: must start with s3://", rawURI)
	}

	trimmed := strings.TrimPrefix(rawURI, "s3://")
	parts := strings.SplitN(trimmed, "/", 2)
	bucket = parts[0]
	if bucket == "" {
		return "", "", fmt.Errorf("invalid S3 URI %q: bucket name cannot be empty", rawURI)
	}

	if len(parts) > 1 {
		prefix = parts[1]
		// Normalize prefix to not have leading slash
		prefix = strings.TrimPrefix(prefix, "/")
	}

	// Unescape URL entities if needed
	if unescaped, err := url.PathUnescape(prefix); err == nil {
		prefix = unescaped
	}

	return bucket, prefix, nil
}
