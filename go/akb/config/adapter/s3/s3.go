package s3

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3svc "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/bkosm/akb/go/akb/config"
)

// S3 is a config.Interface backed by an S3 object. It uses If-Match / ETag
// for optimistic concurrency control; concurrent saves from different processes
// detect conflicts and return config.ErrConflict.
type S3 struct {
	bucket    string
	key       string
	accountID string // resolved by ensureBucket when bucket name defaults to akb-<account-id>
	client    *s3svc.Client
	awsCfg    aws.Config

	mu       sync.Mutex
	lastETag string
}

// New creates an S3 config adapter, resolving the bucket name (defaulting to
// "akb-<account-id>" when empty) and creating the bucket if it does not exist.
func New(ctx context.Context, bucket, key string, awsCfg aws.Config) (*S3, error) {
	s := &S3{bucket: bucket, key: key, client: s3svc.NewFromConfig(awsCfg), awsCfg: awsCfg}
	if err := s.ensureBucket(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *S3) ensureBucket(ctx context.Context) error {
	if s.bucket == "" {
		acctID, err := AccountID(ctx, s.awsCfg)
		if err != nil {
			return fmt.Errorf("resolve default bucket: %w", err)
		}
		s.accountID = acctID
		s.bucket = "akb-" + acctID
	}

	_, err := s.client.HeadBucket(ctx, &s3svc.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})
	if err == nil {
		return nil
	}

	var nf *types.NotFound
	var apiErr smithy.APIError
	isMissing := errors.As(err, &nf) ||
		(errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "NoSuchBucket"))

	if !isMissing {
		return fmt.Errorf("check bucket %q: %w", s.bucket, err)
	}

	createInput := &s3svc.CreateBucketInput{
		Bucket: aws.String(s.bucket),
	}
	// us-east-1 requires no LocationConstraint; every other region requires one.
	if s.awsCfg.Region != "us-east-1" {
		createInput.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(s.awsCfg.Region),
		}
	}
	_, err = s.client.CreateBucket(ctx, createInput)
	if err != nil {
		return fmt.Errorf("create bucket %q: %w", s.bucket, err)
	}

	return nil
}

func (s *S3) Retrieve(ctx context.Context) (config.Config, error) {
	out, err := s.client.GetObject(ctx, &s3svc.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key),
	})
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return s.bootstrap(ctx)
		}
		// Some S3 implementations return a generic 404 instead of NoSuchKey.
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchKey" {
			return s.bootstrap(ctx)
		}
		return config.Config{}, fmt.Errorf("get config from s3://%s/%s: %w", s.bucket, s.key, err)
	}
	defer func() { _ = out.Body.Close() }()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return config.Config{}, fmt.Errorf("read config body: %w", err)
	}

	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return config.Config{}, fmt.Errorf("parse config from s3://%s/%s: %w", s.bucket, s.key, err)
	}

	s.mu.Lock()
	if out.ETag != nil {
		s.lastETag = *out.ETag
	}
	s.mu.Unlock()

	return cfg, nil
}

func (s *S3) bootstrap(ctx context.Context) (config.Config, error) {
	empty := config.Config{
		KBs: make(map[config.Unique]config.KB),
	}
	data, err := json.MarshalIndent(empty, "", "  ")
	if err != nil {
		return config.Config{}, fmt.Errorf("encode empty config: %w", err)
	}

	out, err := s.client.PutObject(ctx, &s3svc.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(s.key),
		Body:        bytes.NewReader(data),
		IfNoneMatch: aws.String("*"),
	})
	if err != nil {
		// Another instance already bootstrapped — re-read.
		if isPreconditionFailed(err) {
			return s.Retrieve(ctx)
		}
		return config.Config{}, fmt.Errorf("bootstrap config at s3://%s/%s: %w", s.bucket, s.key, err)
	}

	s.mu.Lock()
	if out.ETag != nil {
		s.lastETag = *out.ETag
	}
	s.mu.Unlock()

	return empty, nil
}

func (s *S3) Save(ctx context.Context, c config.Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	s.mu.Lock()
	etag := s.lastETag
	s.mu.Unlock()

	input := &s3svc.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key),
		Body:   bytes.NewReader(data),
	}
	if etag != "" {
		input.IfMatch = aws.String(etag)
	}

	out, err := s.client.PutObject(ctx, input)
	if err != nil {
		if isPreconditionFailed(err) {
			return fmt.Errorf("%w", config.ErrConflict)
		}
		return fmt.Errorf("save config to s3://%s/%s: %w", s.bucket, s.key, err)
	}

	s.mu.Lock()
	if out.ETag != nil {
		s.lastETag = *out.ETag
	}
	s.mu.Unlock()

	return nil
}

func isPreconditionFailed(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode() == "PreconditionFailed"
	}
	return false
}

// BackendInfo returns the config storage location as an ARN of the form
// arn:aws:s3:{region}:{account-id}:{bucket}/{key}.
// The account-id segment is empty when the bucket was supplied explicitly
// and the account was never resolved via STS.
func (s *S3) BackendInfo() string {
	return fmt.Sprintf("arn:aws:s3:%s:%s:%s/%s", s.awsCfg.Region, s.accountID, s.bucket, s.key)
}

var _ config.Interface = &S3{}
var _ config.BackendDescriber = &S3{}
