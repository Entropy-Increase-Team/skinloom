package storage

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Options configure an S3-compatible store: Amazon S3, Aliyun OSS, Tencent COS,
// Qiniu Kodo, Cloudflare R2, MinIO and others.
type S3Options struct {
	Endpoint  string // host[:port], e.g. oss-cn-hangzhou.aliyuncs.com
	Bucket    string
	Region    string // needed by some providers; empty = let the client find out
	AccessKey string
	SecretKey string
	Prefix    string // key prefix inside the bucket, e.g. "textures/"
	Insecure  bool   // plain HTTP (a MinIO on the same host)
	PathStyle bool   // the bucket in the path instead of the host name (MinIO, most self-hosted)
	// PublicURL is a base URL serving the bucket, e.g. a CDN in front of it. When set,
	// texture files are redirected there instead of passing through the server.
	PublicURL string
}

// S3 stores files in an S3-compatible bucket, under Prefix + key.
type S3 struct {
	c      *minio.Client
	bucket string
	prefix string
	public string
}

// NewS3 connects to a bucket. It checks the bucket exists, so a wrong endpoint or
// key is reported at startup rather than at the first upload.
func NewS3(ctx context.Context, o S3Options) (*S3, error) {
	lookup := minio.BucketLookupAuto
	if o.PathStyle {
		lookup = minio.BucketLookupPath
	}
	c, err := minio.New(o.Endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(o.AccessKey, o.SecretKey, ""),
		Secure:       !o.Insecure,
		Region:       o.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("s3 storage: %w", err)
	}
	ok, err := c.BucketExists(ctx, o.Bucket)
	if err != nil {
		return nil, fmt.Errorf("s3 storage: checking bucket %q: %w", o.Bucket, err)
	}
	if !ok {
		return nil, fmt.Errorf("s3 storage: bucket %q does not exist", o.Bucket)
	}
	return &S3{c: c, bucket: o.Bucket, prefix: o.Prefix, public: strings.TrimRight(o.PublicURL, "/")}, nil
}

func notFound(err error) bool {
	r := minio.ToErrorResponse(err)
	return r.StatusCode == http.StatusNotFound || r.Code == "NoSuchKey"
}

func (s *S3) object(key string) (string, error) {
	if !ValidKey(key) {
		return "", ErrNotFound
	}
	return s.prefix + key, nil
}

// Headers stored with every object, for clients fetching from the bucket directly
// (PublicURL): a file under a hash never changes.
const (
	s3ContentType  = "image/png"
	s3CacheControl = "public, max-age=31536000, immutable"
)

// Put uploads data, or, if the object exists already, copies it onto itself with new
// metadata: the only way S3 has to make its modification time "now" (see Store). The
// copy replaces the headers too, so they are set again.
func (s *S3) Put(ctx context.Context, key string, data []byte) error {
	name, err := s.object(key)
	if err != nil {
		return fmt.Errorf("invalid storage key %q", key)
	}
	if _, err := s.c.StatObject(ctx, s.bucket, name, minio.StatObjectOptions{}); err == nil {
		_, err = s.c.CopyObject(ctx,
			minio.CopyDestOptions{Bucket: s.bucket, Object: name, ReplaceMetadata: true,
				UserMetadata: map[string]string{"Touched": strconv.FormatInt(time.Now().UnixNano(), 10)},
				ContentType:  s3ContentType, CacheControl: s3CacheControl},
			minio.CopySrcOptions{Bucket: s.bucket, Object: name})
		return err
	} else if !notFound(err) {
		return err
	}
	_, err = s.c.PutObject(ctx, s.bucket, name, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: s3ContentType, CacheControl: s3CacheControl})
	return err
}

// Open opens a stored file for reading; reads and seeks become ranged GETs.
func (s *S3) Open(ctx context.Context, key string) (File, error) {
	name, err := s.object(key)
	if err != nil {
		return nil, err
	}
	obj, err := s.c.GetObject(ctx, s.bucket, name, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	info, err := obj.Stat()
	if err != nil {
		obj.Close()
		if notFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &s3File{Object: obj, info: info}, nil
}

type s3File struct {
	*minio.Object
	info minio.ObjectInfo
}

func (f *s3File) ModTime() time.Time { return f.info.LastModified }
func (f *s3File) Size() int64        { return f.info.Size }

// Stat returns when key was last written.
func (s *S3) Stat(ctx context.Context, key string) (time.Time, error) {
	name, err := s.object(key)
	if err != nil {
		return time.Time{}, err
	}
	info, err := s.c.StatObject(ctx, s.bucket, name, minio.StatObjectOptions{})
	if notFound(err) {
		return time.Time{}, ErrNotFound
	}
	return info.LastModified, err
}

// Walk lists every stored key under the prefix (other objects in the bucket are left
// alone).
func (s *S3) Walk(ctx context.Context, fn func(key string, modTime time.Time) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // stops the listing if fn returns early
	for obj := range s.c.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: s.prefix, Recursive: true}) {
		if obj.Err != nil {
			return obj.Err
		}
		key := strings.TrimPrefix(obj.Key, s.prefix)
		if !ValidKey(key) {
			continue
		}
		if err := fn(key, obj.LastModified); err != nil {
			return err
		}
	}
	return nil
}

// Delete removes a stored file; a missing one is not an error.
func (s *S3) Delete(ctx context.Context, key string) error {
	name, err := s.object(key)
	if err != nil {
		return nil
	}
	err = s.c.RemoveObject(ctx, s.bucket, name, minio.RemoveObjectOptions{})
	if notFound(err) {
		return nil
	}
	return err
}

// URL is where a file can be fetched directly, or "" without a PublicURL.
func (s *S3) URL(key string) string {
	if s.public == "" || !ValidKey(key) {
		return ""
	}
	return s.public + "/" + s.prefix + key
}

// Linker is implemented by stores whose files have a public URL of their own.
type Linker interface {
	URL(key string) string
}

var _ Store = (*S3)(nil)
