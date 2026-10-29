package storage

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeS3 is the part of the S3 API the driver uses, in memory, path-style, for one
// bucket: HEAD bucket, PUT (plain or aws-chunked, or a copy), HEAD/GET with ranges,
// DELETE and ListObjectsV2. Pulling in a full fake (gofakes3) would add the AWS SDK
// to the module graph.
type fakeS3 struct {
	mu      sync.Mutex
	bucket  string
	objects map[string]fakeObject
	copies  int
}

type fakeObject struct {
	data []byte
	mod  time.Time
	// Headers kept with the object. A copy with the REPLACE metadata directive takes
	// them from the request, as S3 does: missing ones are lost.
	contentType, cacheControl string
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/")
	bucket, key, _ := strings.Cut(path, "/")
	if bucket != f.bucket {
		http.Error(w, "no such bucket", http.StatusNotFound)
		return
	}
	if key == "" {
		if r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2" {
			f.list(w, r.URL.Query().Get("prefix"))
			return
		}
		w.WriteHeader(http.StatusOK) // HEAD bucket, GET location
		return
	}
	switch r.Method {
	case http.MethodPut:
		if src := r.Header.Get("X-Amz-Copy-Source"); src != "" {
			_, srcKey, _ := strings.Cut(strings.TrimPrefix(src, "/"), "/")
			o, ok := f.objects[srcKey]
			if !ok {
				noSuchKey(w)
				return
			}
			o.mod = time.Now().Add(time.Duration(f.copies+1) * time.Second) // a visibly newer time
			if strings.EqualFold(r.Header.Get("X-Amz-Metadata-Directive"), "REPLACE") {
				o.contentType, o.cacheControl = r.Header.Get("Content-Type"), r.Header.Get("Cache-Control")
			}
			f.objects[key] = o
			f.copies++
			fmt.Fprintf(w, `<CopyObjectResult><LastModified>%s</LastModified><ETag>"e"</ETag></CopyObjectResult>`, o.mod.UTC().Format(time.RFC3339))
			return
		}
		data, err := readBody(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.objects[key] = fakeObject{data: data, mod: time.Now(),
			contentType: r.Header.Get("Content-Type"), cacheControl: r.Header.Get("Cache-Control")}
		w.Header().Set("ETag", `"e"`)
	case http.MethodHead, http.MethodGet:
		o, ok := f.objects[key]
		if !ok {
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			noSuchKey(w)
			return
		}
		w.Header().Set("ETag", `"e"`)
		w.Header().Set("Content-Type", o.contentType)
		w.Header().Set("Cache-Control", o.cacheControl)
		http.ServeContent(w, r, "", o.mod, bytes.NewReader(o.data))
	case http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	}
}

func noSuchKey(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprint(w, `<Error><Code>NoSuchKey</Code><Message>not found</Message></Error>`)
}

func (f *fakeS3) list(w http.ResponseWriter, prefix string) {
	type content struct {
		Key          string
		LastModified string
		ETag         string
		Size         int
	}
	var keys []string
	for k := range f.objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := struct {
		XMLName     xml.Name `xml:"ListBucketResult"`
		Name        string
		Prefix      string
		KeyCount    int
		IsTruncated bool
		Contents    []content
	}{Name: f.bucket, Prefix: prefix, KeyCount: len(keys)}
	for _, k := range keys {
		o := f.objects[k]
		out.Contents = append(out.Contents, content{k, o.mod.UTC().Format(time.RFC3339Nano), `"e"`, len(o.data)})
	}
	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(out)
}

// readBody reads a PUT body, decoding aws-chunked (signed or unsigned, with trailers).
func readBody(r *http.Request) ([]byte, error) {
	if !strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") {
		return io.ReadAll(r.Body)
	}
	br := bufio.NewReader(r.Body)
	var out []byte
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		sizeHex, _, _ := strings.Cut(strings.TrimSpace(line), ";")
		n, err := strconv.ParseInt(sizeHex, 16, 64)
		if err != nil {
			return nil, fmt.Errorf("chunk size %q: %w", line, err)
		}
		if n == 0 {
			return out, nil // trailers follow; not needed
		}
		chunk := make([]byte, n)
		if _, err := io.ReadFull(br, chunk); err != nil {
			return nil, err
		}
		out = append(out, chunk...)
		if _, err := br.ReadString('\n'); err != nil { // the CRLF after the data
			return nil, err
		}
	}
}

func newS3(t *testing.T, prefix string) (*S3, *fakeS3) {
	t.Helper()
	fake := &fakeS3{bucket: "skins", objects: map[string]fakeObject{}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	s, err := NewS3(context.Background(), S3Options{Endpoint: strings.TrimPrefix(srv.URL, "http://"), Bucket: "skins",
		Region: "us-east-1", AccessKey: "key", SecretKey: "secret", Prefix: prefix, Insecure: true, PathStyle: true,
		PublicURL: "https://cdn.example.com/"})
	if err != nil {
		t.Fatal(err)
	}
	return s, fake
}

func TestS3(t *testing.T) {
	ctx := context.Background()
	s, fake := newS3(t, "textures/")
	key := strings.Repeat("ab", 32)
	data := bytes.Repeat([]byte("skin"), 1000)

	if _, err := s.Open(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := s.Stat(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stat missing: %v", err)
	}
	if err := s.Put(ctx, key, data); err != nil {
		t.Fatal(err)
	}
	if got := fake.objects["textures/"+key].data; !bytes.Equal(got, data) {
		t.Fatalf("stored %d bytes under the prefix", len(got))
	}
	first, err := s.Stat(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	// Storing it again only refreshes its time (copy onto itself), as Local does.
	if err := s.Put(ctx, key, data); err != nil || fake.copies != 1 {
		t.Fatalf("second put: %v, %d copies", err, fake.copies)
	}
	if second, _ := s.Stat(ctx, key); !second.After(first) {
		t.Fatalf("time not refreshed: %v then %v", first, second)
	}
	// The copy replaces the metadata, so it must set the headers again: a CDN serving
	// the bucket (public_url) would otherwise send the file as binary/octet-stream
	// without its cache lifetime once it was uploaded a second time.
	if o := fake.objects["textures/"+key]; o.contentType != "image/png" || !strings.Contains(o.cacheControl, "immutable") {
		t.Fatalf("headers after the refresh: %q, %q", o.contentType, o.cacheControl)
	}

	f, err := s.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Size() != int64(len(data)) || f.ModTime().IsZero() {
		t.Fatalf("size %d, time %v", f.Size(), f.ModTime())
	}
	if _, err := f.Seek(3996, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if tail, _ := io.ReadAll(f); string(tail) != "skin" {
		t.Fatalf("ranged read: %q", tail)
	}

	fake.objects["other/not-ours"] = fakeObject{data: []byte("x"), mod: time.Now()}
	fake.objects["textures/not-a-hash"] = fakeObject{data: []byte("x"), mod: time.Now()}
	var walked []string
	if err := s.Walk(ctx, func(k string, _ time.Time) error { walked = append(walked, k); return nil }); err != nil || len(walked) != 1 || walked[0] != key {
		t.Fatalf("walk: %v, %v", walked, err)
	}
	if got := s.URL(key); got != "https://cdn.example.com/textures/"+key {
		t.Fatalf("URL %q", got)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("deleting a missing file: %v", err)
	}
	if _, err := s.Open(ctx, "../../etc/passwd"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid key: %v", err)
	}
}

func TestS3MissingBucket(t *testing.T) {
	srv := httptest.NewServer(&fakeS3{bucket: "other", objects: map[string]fakeObject{}})
	defer srv.Close()
	_, err := NewS3(context.Background(), S3Options{Endpoint: strings.TrimPrefix(srv.URL, "http://"), Bucket: "skins",
		Region: "us-east-1", AccessKey: "k", SecretKey: "s", Insecure: true, PathStyle: true})
	if err == nil || !strings.Contains(err.Error(), "skins") {
		t.Fatalf("a missing bucket must fail at startup: %v", err)
	}
}
