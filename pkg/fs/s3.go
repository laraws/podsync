package fs

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/gabriel-vasile/mimetype"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// S3Config is the configuration for an S3-compatible storage provider.
// Credentials may be omitted to use the standard AWS credential chain.
type S3Config struct {
	Bucket          string `toml:"bucket"`
	Region          string `toml:"region"`
	EndpointURL     string `toml:"endpoint_url"`
	Prefix          string `toml:"prefix"`
	PublicURL       string `toml:"public_url"`
	AccessKeyID     string `toml:"access_key_id"`
	SecretAccessKey string `toml:"secret_access_key"`
	UsePathStyle    bool   `toml:"use_path_style"`
}

// R2Config is separate in TOML so selecting R2 is explicit while the
// implementation continues to use its S3-compatible API.
type R2Config S3Config

type s3API interface {
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type s3Uploader interface {
	UploadObject(context.Context, *transfermanager.UploadObjectInput, ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error)
}

// S3 implements file storage for S3-compatible providers, including R2.
type S3 struct {
	api      s3API
	uploader s3Uploader
	bucket   string
	prefix   string
}

func NewS3(c S3Config) (*S3, error) {
	options := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(c.Region),
	}
	if c.AccessKeyID != "" || c.SecretAccessKey != "" {
		options = append(options, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(c.AccessKeyID, c.SecretAccessKey, ""),
		))
	}

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(), options...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to initialize S3 configuration")
	}
	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		if c.EndpointURL != "" {
			options.BaseEndpoint = aws.String(c.EndpointURL)
		}
		options.UsePathStyle = c.UsePathStyle
	})
	return &S3{
		api:      client,
		uploader: transfermanager.New(client),
		bucket:   c.Bucket,
		prefix:   c.Prefix,
	}, nil
}

func NewR2(c R2Config) (*S3, error) {
	config := S3Config(c)
	if config.Region == "" {
		config.Region = "auto"
	}
	config.UsePathStyle = true
	return NewS3(config)
}

func (s *S3) Open(_name string) (http.File, error) {
	return nil, errors.New("serving files from S3-compatible storage is not supported")
}

func (s *S3) Delete(ctx context.Context, key string) error {
	_, err := s.api.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if isNotFound(err) {
		return os.ErrNotExist
	}
	return err
}

func (s *S3) Create(ctx context.Context, key string, reader io.Reader) (int64, error) {
	logger := log.WithField("key", key)

	// Detect MIME type from the first 512 bytes and replay them with the rest of the stream.
	buf := make([]byte, 512)
	n, err := io.ReadFull(reader, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return 0, errors.Wrap(err, "failed to read file header for MIME detection")
	}
	head := buf[:n]
	m := mimetype.Detect(head)
	body := &readerWithN{Reader: io.MultiReader(bytes.NewReader(head), reader)}

	logger.Infof("uploading file to %s", s.bucket)
	_, err = s.uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Body:        body,
		Bucket:      aws.String(s.bucket),
		ContentType: aws.String(m.String()),
		Key:         aws.String(key),
	})
	if err != nil {
		return 0, errors.Wrap(err, "failed to upload file")
	}

	logger.Debugf("written %d bytes", body.n)
	return body.n, nil
}

func (s *S3) Size(ctx context.Context, key string) (int64, error) {
	logger := log.WithField("key", key)
	logger.Debugf("getting file size from %s", s.bucket)
	resp, err := s.api.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if isNotFound(err) {
		return 0, os.ErrNotExist
	}
	if err != nil {
		return 0, errors.Wrap(err, "failed to get file size")
	}
	if resp.ContentLength == nil {
		return 0, errors.New("S3 response did not include content length")
	}
	return *resp.ContentLength, nil
}

func (s *S3) ObjectKey(name string) string {
	return strings.TrimPrefix(path.Join("/", s.prefix, name), "/")
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NotFound", "NoSuchKey", "NoSuchBucket":
			return true
		}
	}
	var responseErr *smithyhttp.ResponseError
	return errors.As(err, &responseErr) && responseErr.HTTPStatusCode() == http.StatusNotFound
}

type readerWithN struct {
	io.Reader
	n int64
}

func (r *readerWithN) Read(p []byte) (n int, err error) {
	n, err = r.Reader.Read(p)
	r.n += int64(n)
	return
}
