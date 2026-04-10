package s3

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3svc "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/bkosm/akb/config"
)

// fakeS3 implements just the S3 operations used by the config adapter,
// backed by an in-memory map.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string]fakeObject
}

type fakeObject struct {
	data []byte
	etag string
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: make(map[string]fakeObject)}
}

func (f *fakeS3) GetObject(_ context.Context, input *s3svc.GetObjectInput, _ ...func(*s3svc.Options)) (*s3svc.GetObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := aws.ToString(input.Key)
	obj, ok := f.objects[key]
	if !ok {
		return nil, &types.NoSuchKey{}
	}
	return &s3svc.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader(obj.data)),
		ETag: aws.String(obj.etag),
	}, nil
}

func (f *fakeS3) PutObject(_ context.Context, input *s3svc.PutObjectInput, _ ...func(*s3svc.Options)) (*s3svc.PutObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := aws.ToString(input.Key)

	if ifNone := aws.ToString(input.IfNoneMatch); ifNone == "*" {
		if _, exists := f.objects[key]; exists {
			return nil, &preconditionFailedError{}
		}
	}

	if ifMatch := aws.ToString(input.IfMatch); ifMatch != "" {
		obj, exists := f.objects[key]
		if !exists || obj.etag != ifMatch {
			return nil, &preconditionFailedError{}
		}
	}

	data, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}

	newETag := fmt.Sprintf(`"%x"`, len(f.objects)+1)
	f.objects[key] = fakeObject{data: data, etag: newETag}
	return &s3svc.PutObjectOutput{ETag: aws.String(newETag)}, nil
}

type preconditionFailedError struct{}

func (e *preconditionFailedError) Error() string     { return "PreconditionFailed" }
func (e *preconditionFailedError) ErrorCode() string { return "PreconditionFailed" }
func (e *preconditionFailedError) ErrorMessage() string {
	return "At least one of the pre-conditions you specified did not hold"
}
func (e *preconditionFailedError) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

type s3API interface {
	GetObject(context.Context, *s3svc.GetObjectInput, ...func(*s3svc.Options)) (*s3svc.GetObjectOutput, error)
	PutObject(context.Context, *s3svc.PutObjectInput, ...func(*s3svc.Options)) (*s3svc.PutObjectOutput, error)
}

type testS3 struct {
	Bucket string
	Key    string
	api    s3API

	mu       sync.Mutex
	lastETag string
}

func (s *testS3) Retrieve(ctx context.Context) (config.Config, error) {
	out, err := s.api.GetObject(ctx, &s3svc.GetObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(s.Key),
	})
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return s.bootstrap(ctx)
		}
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchKey" {
			return s.bootstrap(ctx)
		}
		return config.Config{}, fmt.Errorf("get config: %w", err)
	}
	defer func() { _ = out.Body.Close() }()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return config.Config{}, err
	}

	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return config.Config{}, err
	}

	s.mu.Lock()
	if out.ETag != nil {
		s.lastETag = *out.ETag
	}
	s.mu.Unlock()

	return cfg, nil
}

func (s *testS3) bootstrap(ctx context.Context) (config.Config, error) {
	empty := config.Config{
		KBs: make(map[config.Unique]config.KB),
	}
	data, _ := json.MarshalIndent(empty, "", "  ")

	out, err := s.api.PutObject(ctx, &s3svc.PutObjectInput{
		Bucket:      aws.String(s.Bucket),
		Key:         aws.String(s.Key),
		Body:        bytes.NewReader(data),
		IfNoneMatch: aws.String("*"),
	})
	if err != nil {
		if isPreconditionFailed(err) {
			return s.Retrieve(ctx)
		}
		return config.Config{}, err
	}

	s.mu.Lock()
	if out.ETag != nil {
		s.lastETag = *out.ETag
	}
	s.mu.Unlock()

	return empty, nil
}

func (s *testS3) Save(ctx context.Context, c config.Config) error {
	data, _ := json.MarshalIndent(c, "", "  ")

	s.mu.Lock()
	etag := s.lastETag
	s.mu.Unlock()

	input := &s3svc.PutObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(s.Key),
		Body:   bytes.NewReader(data),
	}
	if etag != "" {
		input.IfMatch = aws.String(etag)
	}

	out, err := s.api.PutObject(ctx, input)
	if err != nil {
		if isPreconditionFailed(err) {
			return fmt.Errorf("%w", config.ErrConflict)
		}
		return err
	}

	s.mu.Lock()
	if out.ETag != nil {
		s.lastETag = *out.ETag
	}
	s.mu.Unlock()

	return nil
}

func makeAdapter(fake *fakeS3, bucket, key string) *testS3 {
	return &testS3{Bucket: bucket, Key: key, api: fake}
}

func TestRetrieve_existingConfig(t *testing.T) {
	t.Parallel()
	fake := newFakeS3()
	cfg := config.Config{
		KBs: map[config.Unique]config.KB{"kb1": {Mount: "/tmp/kb"}},
	}
	data, _ := json.Marshal(cfg)
	fake.objects["config.json"] = fakeObject{data: data, etag: `"abc"`}

	a := makeAdapter(fake, "bucket", "config.json")
	got, err := a.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(got.KBs) != 1 {
		t.Fatalf("len(KBs) = %d, want 1", len(got.KBs))
	}
	if got.KBs["kb1"].Mount != "/tmp/kb" {
		t.Fatalf("unexpected mount: %s", got.KBs["kb1"].Mount)
	}

	a.mu.Lock()
	etag := a.lastETag
	a.mu.Unlock()
	if etag != `"abc"` {
		t.Fatalf("lastETag = %q, want %q", etag, `"abc"`)
	}
}

func TestRetrieve_missingBootstraps(t *testing.T) {
	t.Parallel()
	fake := newFakeS3()
	a := makeAdapter(fake, "bucket", "config.json")

	got, err := a.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if got.KBs == nil {
		t.Fatal("expected non-nil KBs map")
	}
	if len(got.KBs) != 0 {
		t.Fatalf("expected empty config, got %d kbs", len(got.KBs))
	}

	if _, ok := fake.objects["config.json"]; !ok {
		t.Fatal("bootstrap did not write config.json to fake S3")
	}
}

func TestRetrieve_malformedJSON(t *testing.T) {
	t.Parallel()
	fake := newFakeS3()
	fake.objects["config.json"] = fakeObject{data: []byte(`{not json`), etag: `"x"`}

	a := makeAdapter(fake, "bucket", "config.json")
	_, err := a.Retrieve(context.Background())
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestSave_success(t *testing.T) {
	t.Parallel()
	fake := newFakeS3()
	a := makeAdapter(fake, "bucket", "config.json")

	_, err := a.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	a.mu.Lock()
	etagBefore := a.lastETag
	a.mu.Unlock()

	cfg := config.Config{
		KBs: map[config.Unique]config.KB{"new": {Mount: "/new"}},
	}
	if err := a.Save(context.Background(), cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	a.mu.Lock()
	etagAfter := a.lastETag
	a.mu.Unlock()
	if etagAfter == etagBefore {
		t.Fatal("expected ETag to change after Save")
	}

	got, err := a.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve after Save: %v", err)
	}
	if got.KBs["new"].Mount != "/new" {
		t.Fatalf("unexpected kb: %#v", got.KBs["new"])
	}
}

func TestSave_conflict(t *testing.T) {
	t.Parallel()
	fake := newFakeS3()
	a := makeAdapter(fake, "bucket", "config.json")

	_, err := a.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	fake.mu.Lock()
	obj := fake.objects["config.json"]
	obj.etag = `"changed-by-other"`
	fake.objects["config.json"] = obj
	fake.mu.Unlock()

	cfg := config.Config{
		KBs: map[config.Unique]config.KB{},
	}
	err = a.Save(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if !errors.Is(err, config.ErrConflict) {
		t.Fatalf("expected ErrConflict, got: %v", err)
	}
}

func TestBootstrap_raceSecondReaderWins(t *testing.T) {
	t.Parallel()
	fake := newFakeS3()

	cfg := config.Config{
		KBs: map[config.Unique]config.KB{"existing": {Mount: "/e"}},
	}
	data, _ := json.Marshal(cfg)
	fake.objects["config.json"] = fakeObject{data: data, etag: `"first"`}

	a := makeAdapter(fake, "bucket", "config.json")
	got, err := a.bootstrap(context.Background())
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if got.KBs["existing"].Mount != "/e" {
		t.Fatalf("expected to read existing config, got: %#v", got.KBs)
	}
}
