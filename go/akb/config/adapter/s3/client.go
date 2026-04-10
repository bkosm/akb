package s3

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	s3svc "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// LoadConfig loads the default AWS config.
// If region is non-empty it overrides the SDK default.
func LoadConfig(ctx context.Context, region string) (aws.Config, error) {
	var opts []func(*awsconfig.LoadOptions) error
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("load aws config: %w", err)
	}
	return cfg, nil
}

// NewClient creates an S3 client using the default AWS credential chain.
// If region is non-empty it overrides the SDK default.
func NewClient(ctx context.Context, region string) (*s3svc.Client, error) {
	cfg, err := LoadConfig(ctx, region)
	if err != nil {
		return nil, err
	}
	return s3svc.NewFromConfig(cfg), nil
}

// AccountID returns the AWS account ID of the caller via STS GetCallerIdentity.
func AccountID(ctx context.Context, cfg aws.Config) (string, error) {
	out, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("get caller identity: %w", err)
	}
	return *out.Account, nil
}
