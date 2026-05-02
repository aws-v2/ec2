package storage

import (
	"context"
	"fmt"

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

func (m *MinIOAdapter) DownloadFile(ctx context.Context, bucket, key, destPath string) error {
	err := m.client.FGetObject(ctx, bucket, key, destPath, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to download %s/%s: %w", bucket, key, err)
	}
	return nil
}