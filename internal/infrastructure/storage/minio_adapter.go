package storage

import (
	"context"
	"fmt"
	"io"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinIOAdapter struct {
	client *minio.Client
}

func NewMinIOAdapter(endpoint, accessKey, secretKey string, useSSL bool) (*MinIOAdapter, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO client: %w", err)
	}

	return &MinIOAdapter{client: client}, nil
}

// DownloadFile downloads an object from a bucket to a local file path.
// This is more efficient than reading into memory because it streams directly to disk.
func (m *MinIOAdapter) DownloadFile(ctx context.Context, bucket, key, destPath string) error {
	err := m.client.FGetObject(ctx, bucket, key, destPath, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to download file from minio: %w", err)
	}
	return nil
}

// GetObjectStream returns an io.ReadCloser for the object.
// The caller is responsible for closing the stream.
func (m *MinIOAdapter) GetObjectStream(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	object, err := m.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get object stream: %w", err)
	}
	return object, nil
}

// GetObjectInfo returns metadata about an object, such as size and content type.
func (m *MinIOAdapter) GetObjectInfo(ctx context.Context, bucket, key string) (minio.ObjectInfo, error) {
	return m.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
}
