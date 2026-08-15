package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// uploadVideoToS3 stores the video in S3 under a random key with the given
// prefix and returns the CloudFront URL for the object.
func (cfg *apiConfig) uploadVideoToS3(ctx context.Context, video io.Reader, contentType, keyPrefix, fileExtension string) (string, error) {
	key, err := randomObjectKey(keyPrefix, fileExtension)
	if err != nil {
		return "", err
	}

	_, err = cfg.s3Config.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &key,
		Body:        video,
		ContentType: &contentType,
	})
	if err != nil {
		return "", fmt.Errorf("couldn't upload file to S3: %w", err)
	}

	return fmt.Sprintf("https://%s.cloudfront.net/%s", cfg.s3CfDistribution, key), nil
}

// randomObjectKey returns a random object key under keyPrefix with the given
// file extension.
func randomObjectKey(keyPrefix, fileExtension string) (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("couldn't generate random bytes: %w", err)
	}

	return keyPrefix + "/" + base64.RawURLEncoding.EncodeToString(bytes) + fileExtension, nil
}
