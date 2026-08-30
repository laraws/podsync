package fs

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestS3_Create(t *testing.T) {
	files := make(map[string][]byte)
	stor := newMockS3(files, "")

	written, err := stor.Create(testCtx, "1/test", bytes.NewBuffer([]byte{1, 5, 7, 8, 3}))
	require.NoError(t, err)
	assert.EqualValues(t, 5, written)
	assert.Equal(t, []byte{1, 5, 7, 8, 3}, files["1/test"])
}

func TestS3_Size(t *testing.T) {
	stor := newMockS3(map[string][]byte{"1/test": {1, 5, 7, 8, 3}}, "")
	sz, err := stor.Size(testCtx, "1/test")
	require.NoError(t, err)
	assert.EqualValues(t, 5, sz)
}

func TestS3_NoSize(t *testing.T) {
	stor := newMockS3(map[string][]byte{}, "")
	_, err := stor.Size(testCtx, "1/test")
	assert.True(t, os.IsNotExist(err))
}

func TestS3_Delete(t *testing.T) {
	files := map[string][]byte{"1/test": {1, 5, 7, 8, 3}}
	stor := newMockS3(files, "")

	require.NoError(t, stor.Delete(testCtx, "1/test"))
	_, ok := files["1/test"]
	assert.False(t, ok)
	_, err := stor.Size(testCtx, "1/test")
	assert.True(t, errors.Is(err, os.ErrNotExist))
}

func TestS3_ObjectKey(t *testing.T) {
	assert.Equal(t, "test-fn", newMockS3(nil, "").ObjectKey("test-fn"))
	assert.Equal(t, "mock-prefix/test-fn", newMockS3(nil, "mock-prefix").ObjectKey("/test-fn"))
}

type mockS3 struct {
	files map[string][]byte
}

func newMockS3(files map[string][]byte, prefix string) *S3 {
	api := &mockS3{files: files}
	return &S3{api: api, uploader: api, bucket: "mock-bucket", prefix: prefix}
}

func (m *mockS3) UploadObject(_ context.Context, input *transfermanager.UploadObjectInput, _ ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error) {
	content, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	m.files[aws.ToString(input.Key)] = content
	return &transfermanager.UploadObjectOutput{}, nil
}

func (m *mockS3) HeadObject(_ context.Context, input *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	content, ok := m.files[aws.ToString(input.Key)]
	if !ok {
		return nil, &smithy.GenericAPIError{Code: "NotFound", Message: "not found"}
	}
	return &s3.HeadObjectOutput{ContentLength: aws.Int64(int64(len(content)))}, nil
}

func (m *mockS3) DeleteObject(_ context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	key := aws.ToString(input.Key)
	if _, ok := m.files[key]; !ok {
		return nil, &smithy.GenericAPIError{Code: "NotFound", Message: "not found"}
	}
	delete(m.files, key)
	return &s3.DeleteObjectOutput{}, nil
}
