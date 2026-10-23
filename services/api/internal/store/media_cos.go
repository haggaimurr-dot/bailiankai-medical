package store

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	cos "github.com/tencentyun/cos-go-sdk-v5"
)

type COSConfig struct {
	Bucket, Region, Prefix, SecretID, SecretKey, SessionToken string
}

type COSMedia struct {
	client *cos.Client
	prefix string
}

func NewCOSMedia(c COSConfig) (*COSMedia, error) {
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]*-[0-9]+$`).MatchString(c.Bucket) || !regexp.MustCompile(`^[a-z]{2}-[a-z]+(?:-[0-9]+)?$`).MatchString(c.Region) {
		return nil, errors.New("COS requires a bucket-appid and region")
	}
	if c.SecretID == "" || c.SecretKey == "" {
		return nil, errors.New("COS credentials are required")
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]+(?:/[a-zA-Z0-9_-]+)*$`).MatchString(c.Prefix) {
		return nil, errors.New("COS prefix must be a non-empty safe path")
	}
	u, _ := url.Parse(fmt.Sprintf("https://%s.cos.%s.myqcloud.com", c.Bucket, c.Region))
	client := cos.NewClient(&cos.BaseURL{BucketURL: u}, &http.Client{
		Timeout:   15 * time.Second,
		Transport: &cos.AuthorizationTransport{SecretID: c.SecretID, SecretKey: c.SecretKey, SessionToken: c.SessionToken, Transport: http.DefaultTransport},
		// Object requests must never follow redirects to public or foreign endpoints.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	})
	return &COSMedia{client: client, prefix: c.Prefix + "/"}, nil
}

func (m *COSMedia) key(id string) (string, error) {
	if !validMediaID(id) {
		return "", errors.New("invalid media id")
	}
	return m.prefix + id + ".jpg", nil
}

// Ready fails closed for public ACLs/policies and disabled versioning. The
// deployment must also enable COS Block Public Access and forbid policy changes
// to the API identity; ACL checks alone cannot override cloud administrator IAM.
func (m *COSMedia) Ready(ctx context.Context) error {
	acl, _, err := m.client.Bucket.GetACL(ctx)
	if err != nil {
		return errors.New("COS private bucket ACL check failed")
	}
	for _, grant := range acl.AccessControlList {
		if grant.Grantee != nil && (grant.Grantee.URI != "" || grant.Grantee.Type == "Group" || strings.Contains(grant.Grantee.ID, "anyone")) {
			return errors.New("COS bucket has a public ACL")
		}
	}
	policy, _, err := m.client.Bucket.GetPolicy(ctx)
	if err != nil {
		var e *cos.ErrorResponse
		if !errors.As(err, &e) || e.Code != "NoSuchBucketPolicy" {
			return errors.New("COS bucket policy check failed")
		}
	} else {
		for _, statement := range policy.Statement {
			if !strings.EqualFold(statement.Effect, "allow") {
				continue
			}
			principal := statement.Principal
			if len(principal) == 0 {
				principal = policy.Principal
			}
			if len(principal) == 0 {
				return errors.New("COS allow policy has no private principal")
			}
			for _, values := range principal {
				for _, value := range values {
					if strings.Contains(value, "*") || strings.Contains(value, "anyone") {
						return errors.New("COS bucket policy permits public access")
					}
				}
			}
		}
	}
	version, _, err := m.client.Bucket.GetVersioning(ctx)
	if err != nil || version.Status != "Enabled" {
		return errors.New("COS bucket versioning must be enabled")
	}
	return nil
}

func (m *COSMedia) Save(ctx context.Context, id string, data []byte) error {
	key, err := m.key(id)
	if err != nil {
		return err
	}
	if len(data) == 0 || len(data) > MaxMediaBytes {
		return errors.New("invalid media size")
	}
	hash := md5.Sum(data)
	_, err = m.client.Object.Put(ctx, key, bytes.NewReader(data), &cos.ObjectPutOptions{
		ACLHeaderOptions:       &cos.ACLHeaderOptions{XCosACL: "private"},
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{ContentType: "application/octet-stream", CacheControl: "no-store", ContentLength: int64(len(data)), ContentMD5: base64.StdEncoding.EncodeToString(hash[:]), XCosServerSideEncryption: "AES256"},
	})
	if err != nil {
		return fmt.Errorf("COS upload failed: %w", err)
	}
	return nil
}

func (m *COSMedia) Read(ctx context.Context, id string) ([]byte, error) {
	key, err := m.key(id)
	if err != nil {
		return nil, err
	}
	response, err := m.client.Object.Get(ctx, key, nil)
	if err != nil {
		var e *cos.ErrorResponse
		if errors.As(err, &e) && e.Code == "NoSuchKey" {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("COS read failed: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxMediaBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxMediaBytes {
		return nil, errors.New("COS object exceeds media limit")
	}
	return data, nil
}

func (m *COSMedia) Delete(ctx context.Context, id string) error {
	key, err := m.key(id)
	if err != nil {
		return err
	}
	_, err = m.client.Object.Delete(ctx, key, nil) // Versioned bucket: retain historical bytes for recovery.
	return err
}

func (m *COSMedia) List(ctx context.Context, visit func(MediaObject) error) error {
	marker := ""
	for {
		page, _, err := m.client.Bucket.Get(ctx, &cos.BucketGetOptions{Prefix: m.prefix, Marker: marker, MaxKeys: 1000})
		if err != nil {
			return err
		}
		for _, object := range page.Contents {
			if !strings.HasPrefix(object.Key, m.prefix) || !strings.HasSuffix(object.Key, ".jpg") {
				continue
			}
			id := strings.TrimSuffix(strings.TrimPrefix(object.Key, m.prefix), ".jpg")
			if !validMediaID(id) {
				continue
			} // Exclude other prefixes, including snapshots.
			modified, err := time.Parse(time.RFC3339, object.LastModified)
			if err != nil {
				return err
			}
			if err := visit(MediaObject{ID: id, ModifiedAt: modified}); err != nil {
				return err
			}
		}
		if !page.IsTruncated {
			return nil
		}
		if page.NextMarker == "" || page.NextMarker <= marker {
			return errors.New("COS listing did not advance")
		}
		marker = page.NextMarker
	}
}
