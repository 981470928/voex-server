package filestore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"voex-server/internal/shared"
)

// Download carries a previously authorized object and its response policy.
type Download struct {
	Key, Name, MIME, Cache, Missing string
	Inline, SameOrigin              bool
}

// Serve preserves the HTTP file API while keeping the R2 bucket private.
func (s *Store) Serve(w http.ResponseWriter, r *http.Request, download Download) {
	metadata, err := s.client.HeadObject(r.Context(), &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(download.Key),
	})
	if err != nil {
		if storageStatus(err) == http.StatusNotFound {
			shared.Fail(http.StatusNotFound, download.Missing)
		}
		storageFailure("head object", err)
	}
	if metadata.ContentLength == nil || *metadata.ContentLength < 0 {
		storageFailure("head object", errors.New("missing or invalid object size"))
	}
	ctx, cancel := context.WithCancel(r.Context())
	content := &objectReader{
		ctx: ctx, cancel: cancel, client: s.client, bucket: s.bucket,
		key: download.Key, etag: metadata.ETag, size: *metadata.ContentLength,
	}
	defer content.Close()
	if download.SameOrigin {
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	}
	mimeType := download.MIME
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	disposition := "attachment"
	if download.Inline {
		disposition = "inline"
	}
	encoded := strings.ReplaceAll(url.QueryEscape(download.Name), "+", "%20")
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", download.Cache)
	w.Header().Set("Content-Disposition", disposition+"; filename*=UTF-8''"+encoded)
	if etag := aws.ToString(metadata.ETag); etag != "" {
		w.Header().Set("ETag", etag)
	}
	response := &downloadResponse{ResponseWriter: w}
	// Ignore excessive ranges rather than multiplying one request into thousands
	// of R2 reads. HTTP allows a server to send the complete representation instead.
	if ranges := r.Header.Get("Range"); strings.HasPrefix(ranges, "bytes=") && strings.Count(ranges, ",") >= 16 {
		r = r.Clone(r.Context())
		r.Header.Del("Range")
	}
	http.ServeContent(response, r, download.Name, aws.ToTime(metadata.LastModified), content)
	// Multi-range responses read in a ServeContent goroutine. Cancel first, then
	// synchronize with it before examining errors or closing its upstream body.
	_ = content.Close()
	if response.writeErr != nil {
		panic(http.ErrAbortHandler)
	}
	err = content.failure()
	if err == nil && r.Method != http.MethodHead && (response.status == http.StatusOK || response.status == http.StatusPartialContent) {
		if expected, parseErr := strconv.ParseInt(response.Header().Get("Content-Length"), 10, 64); parseErr == nil && response.written != expected {
			err = io.ErrUnexpectedEOF
		}
	}
	if err != nil {
		if response.committed {
			logStorageError("read object", err)
			panic(http.ErrAbortHandler)
		}
		// The first upstream GET may fail after HEAD succeeded. No bytes have
		// reached the client yet, so Recovery can still produce its JSON error.
		for _, header := range []string{"Content-Type", "Content-Length", "Content-Range", "Content-Disposition", "ETag", "Last-Modified", "Accept-Ranges"} {
			w.Header().Del(header)
		}
		if storageStatus(err) == http.StatusNotFound {
			shared.Fail(http.StatusNotFound, download.Missing)
		}
		storageFailure("read object", err)
	}
	response.commit()
}

// objectReader opens one ranged GET per seek, rather than one GET per Read.
// ServeContent calls Read/Seek in a goroutine for multipart ranges, so these
// methods must return errors instead of using the application's panic helpers.
type objectReader struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	client *s3.Client
	bucket string
	key    string
	etag   *string
	size   int64
	offset int64
	body   io.ReadCloser
	err    error
	closed bool
}

func (o *objectReader) Read(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return 0, io.ErrClosedPipe
	}
	if len(p) == 0 {
		return 0, nil
	}
	if o.offset >= o.size {
		return 0, io.EOF
	}
	if o.err != nil {
		return 0, o.err
	}
	if o.body == nil {
		object, err := o.client.GetObject(o.ctx, &s3.GetObjectInput{
			Bucket: aws.String(o.bucket), Key: aws.String(o.key), IfMatch: o.etag,
			Range: aws.String(fmt.Sprintf("bytes=%d-", o.offset)),
		})
		if err != nil {
			o.err = err
			return 0, err
		}
		o.body = object.Body
		if object.Body == nil || object.ContentLength == nil || *object.ContentLength != o.size-o.offset {
			o.err = errors.New("unexpected ranged object size")
			return 0, o.err
		}
	}
	if remaining := o.size - o.offset; int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := o.body.Read(p)
	o.offset += int64(n)
	if errors.Is(err, io.EOF) && o.offset < o.size {
		err = io.ErrUnexpectedEOF
	}
	if err != nil && !errors.Is(err, io.EOF) {
		o.err = err
	}
	return n, err
}

func (o *objectReader) Seek(offset int64, whence int) (int64, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return 0, io.ErrClosedPipe
	}
	var base int64
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		base = o.offset
	case io.SeekEnd:
		base = o.size
	default:
		return 0, errors.New("invalid seek origin")
	}
	next := base + offset
	if next < 0 || (offset > 0 && next < base) || (offset < 0 && next > base) {
		return 0, errors.New("invalid seek offset")
	}
	if next != o.offset {
		if o.body != nil {
			_ = o.body.Close()
			o.body = nil
		}
		o.offset = next
	}
	return o.offset, nil
}

func (o *objectReader) Close() error {
	o.cancel()
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closed = true
	if o.body != nil {
		err := o.body.Close()
		o.body = nil
		return err
	}
	return nil
}

func (o *objectReader) failure() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.err
}

// Delay headers until the first body write so initial upstream failures can
// become API errors. HEAD, empty files and conditional responses commit at end.
type downloadResponse struct {
	http.ResponseWriter
	status    int
	written   int64
	committed bool
	writeErr  error
}

func (w *downloadResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *downloadResponse) Write(p []byte) (int, error) {
	w.commit()
	n, err := w.ResponseWriter.Write(p)
	w.written += int64(n)
	if err != nil {
		w.writeErr = err
	} else if n != len(p) {
		w.writeErr = io.ErrShortWrite
	}
	return n, err
}

func (w *downloadResponse) commit() {
	if !w.committed {
		if w.status == 0 {
			w.status = http.StatusOK
		}
		w.committed = true
		w.ResponseWriter.WriteHeader(w.status)
	}
}
