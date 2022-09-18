package internal

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/hashicorp/go-multierror"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/determined-ai/determined/master/internal/api"
	"github.com/determined-ai/determined/master/pkg/ptrs"
	"github.com/determined-ai/determined/master/pkg/schemas/expconf"
)

const (
	// MIMEApplicationGZip is GZip's MIME type.
	MIMEApplicationGZip = "application/gzip"
	// MIMEApplicationZip is Zip's MIME type.
	MIMEApplicationZip = "application/zip"
)

func storageConfig2Str(config any) string {
	switch config.(type) {
	case expconf.AzureConfig:
		return "azure"
	case expconf.GCSConfig:
		return "gcs"
	case expconf.HDFSConfig:
		return "hdfs"
	case expconf.S3Config:
		return "s3"
	case expconf.SharedFSConfig:
		return "shared_fs"
	default:
		return "unknown"
	}
}

type archiveWriter interface {
	WriteHeader(path string, size int64) error
	Write(b []byte) (int, error)
	Close() error
}

type archiveClosers struct {
	closers []io.Closer
}

// Close() closes all items in closers in reverse order.
func (ac *archiveClosers) Close() error {
	for i := len(ac.closers) - 1; i >= 0; i-- {
		err := ac.closers[i].Close()
		if err != nil {
			return err
		}
	}
	return nil
}

type tarArchiveWriter struct {
	archiveClosers
	tw *tar.Writer
}

func (aw *tarArchiveWriter) WriteHeader(path string, size int64) error {
	hdr := tar.Header{
		Name: path,
		Mode: 0666,
		Size: size,
	}
	if strings.HasSuffix(path, "/") {
		// This a directory
		hdr.Mode = 0777
	}
	return aw.tw.WriteHeader(&hdr)
}

func (aw *tarArchiveWriter) Write(p []byte) (int, error) {
	return aw.tw.Write(p)
}

type zipArchiveWriter struct {
	archiveClosers
	zw        *zip.Writer
	zwContent io.Writer
}

func (aw *zipArchiveWriter) WriteHeader(path string, size int64) error {
	// Zip by default sets mode 0666 and 0777 for files and folders respectively
	zwc, err := aw.zw.Create(path)
	if err != nil {
		return err
	}
	aw.zwContent = zwc
	return nil
}

func (aw *zipArchiveWriter) Write(p []byte) (int, error) {
	var w io.Writer
	// Guard against the mistake where WriteHeader() is not called before
	// calling Write(). The AWS SDK likely will not make this mistake but
	// zipArchiveWriter is not just limited to being used with AWS.
	if aw.zwContent == nil {
		return 0, nil
	}
	w = aw.zwContent
	return w.Write(p)
}

type delayWriter struct {
	delayBytes int
	buf        []byte
	next       io.Writer
}

func (w *delayWriter) Write(p []byte) (int, error) {
	if w.buf != nil {
		if len(w.buf)+len(p) < w.delayBytes {
			// Not enough bytes yet, just buffer them and wait
			w.buf = append(w.buf, p...)
			return len(p), nil
		}
		// Enough bytes, flush buffered bytes, and then write current bytes
		_, err := w.next.Write(w.buf)
		w.buf = nil
		if err != nil {
			return 0, err
		}
	}
	return w.next.Write(p)
}

// Close flushes the buffer if it is nonempty.
func (w *delayWriter) Close() error {
	if w.buf != nil && len(w.buf) > 0 {
		_, err := w.next.Write(w.buf)
		return err
	}
	return nil
}

func newDelayWriter(w io.Writer, delayBytes int) *delayWriter {
	return &delayWriter{
		delayBytes: delayBytes,
		buf:        make([]byte, 0, delayBytes),
		next:       w,
	}
}

// seqWriterAt satisfies S3 APIs' io.WriterAt interface while staying sequential.
// To use it with s3manager.Downloader, its concurrency needs be set to 1.
// Ref: https://docs.aws.amazon.com/sdk-for-go/api/service/s3/s3manager/#Downloader
type seqWriterAt struct {
	next    io.Writer
	written int64
}

func newSeqWriterAt(w io.Writer) *seqWriterAt {
	return &seqWriterAt{next: w}
}

// WriteAt writes the content in buffer p.
func (w *seqWriterAt) WriteAt(p []byte, off int64) (int, error) {
	if off != w.written {
		return 0, fmt.Errorf(
			"only supporting sequential writes,"+
				" writing at offset %d while %d bytes have been written",
			off, w.written)
	}
	n, err := w.next.Write(p)
	w.written += int64(n)
	if err != nil {
		return 0, err
	}

	return n, err
}

// BatchDownloadIterator implements s3's BatchDownloadIterator API.
type batchDownloadIterator struct {
	// The objects we are writing
	objects []*s3.Object
	// The output we are writing to
	aw archiveWriter
	// Internal states
	err    error
	pos    int
	bucket string
	prefix string
}

// Next() returns true if the next item is available.
func (i *batchDownloadIterator) Next() bool {
	i.pos++
	if i.pos == len(i.objects) {
		return false
	}
	pathname := strings.TrimPrefix(*i.objects[i.pos].Key, i.prefix)
	err := i.aw.WriteHeader(pathname, *i.objects[i.pos].Size)
	if err != nil {
		i.err = err
		return false
	}
	return true
}

// Err() eturns the error if any.
func (i *batchDownloadIterator) Err() error {
	return i.err
}

// DownloadObject() eturns a DownloadObject.
func (i *batchDownloadIterator) DownloadObject() s3manager.BatchDownloadObject {
	return s3manager.BatchDownloadObject{
		Object: &s3.GetObjectInput{
			Bucket: &i.bucket,
			Key:    i.objects[i.pos].Key,
		},
		Writer: newSeqWriterAt(i.aw),
	}
}

func newBatchDownloadIterator(aw archiveWriter,
	bucket string, prefix string, objs []*s3.Object) *batchDownloadIterator {
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return &batchDownloadIterator{
		aw:      aw,
		bucket:  bucket,
		prefix:  prefix,
		objects: objs,
		pos:     -1,
	}
}

func getS3BucketRegion(ctx context.Context, bucket string) (string, error) {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("us-west-2"),
	})
	if err != nil {
		return "", nil
	}

	out, err := s3.New(sess).GetBucketLocationWithContext(ctx, &s3.GetBucketLocationInput{
		Bucket: &bucket,
	})
	if err != nil {
		return "", err
	}

	return *out.LocationConstraint, nil
}

type checkpointDownloader interface {
	download(ctx context.Context) error
}

type s3Downloader struct {
	aw     archiveWriter
	bucket string
	prefix string
}

func (d *s3Downloader) download(ctx context.Context) error {
	region, err := getS3BucketRegion(ctx, d.bucket)
	if err != nil {
		return err
	}
	sess, err := session.NewSession(&aws.Config{
		Region: &region,
	})
	if err != nil {
		return err
	}
	// We do not pass in credentials explicitly. Instead, we reply on
	// the existing AWS credentials.
	s3client := s3.New(sess)

	var merr error
	downloader := s3manager.NewDownloader(sess, func(d *s3manager.Downloader) {
		d.Concurrency = 1 // Setting concurrency to 1 to use seqWriterAt
	})
	funcReadPage := func(output *s3.ListObjectsV2Output, lastPage bool) bool {
		iter := newBatchDownloadIterator(d.aw, d.bucket, d.prefix, output.Contents)
		// Download every bucket in this page
		err = downloader.DownloadWithIterator(ctx, iter)
		if iter.Err() != nil {
			merr = multierror.Append(merr, iter.Err())
		}
		if err != nil {
			merr = multierror.Append(merr, err)
		}

		// Return False to stop paging
		return merr == nil
	}
	err = s3client.ListObjectsV2PagesWithContext(
		ctx,
		&s3.ListObjectsV2Input{
			Bucket: &d.bucket,
			Prefix: &d.prefix,
		},
		funcReadPage,
	)
	if err != nil {
		merr = multierror.Append(merr, err)
	}
	if merr != nil {
		return fmt.Errorf("one or more errors encountered during checkpoint download: %w", merr)
	}
	return nil
}

func newDownloader(
	storageConfig *expconf.CheckpointStorageConfig,
	aw archiveWriter,
	id string,
) (checkpointDownloader, error) {
	switch storage := storageConfig.GetUnionMember().(type) {
	case expconf.S3Config:
		return &s3Downloader{
			aw:     aw,
			bucket: storage.Bucket(),
			prefix: strings.TrimLeft(*storage.Prefix()+"/"+id, "/"),
		}, nil
	default:
		return nil, echo.NewHTTPError(http.StatusNotImplemented,
			fmt.Sprintf("checkpoint download via master is only supported on S3"+
				", but the checkpoint's storage type is %s", storageConfig2Str(storage)))
	}
}

// It is assumed that a http status code is not sent until the first write to w.
func buildWriterPipeline(w io.Writer, mimeType string) (archiveWriter, error) {
	// DelayWriter delays the first write until we have successfully downloaded
	// some bytes and are more confident that the download will succeed.
	dw := newDelayWriter(w, 16*1024)
	closers := []io.Closer{dw}
	switch mimeType {
	case MIMEApplicationGZip:
		gz := gzip.NewWriter(dw)
		closers = append(closers, gz)

		tw := tar.NewWriter(gz)
		closers = append(closers, tw)

		return &tarArchiveWriter{archiveClosers{closers}, tw}, nil

	case MIMEApplicationZip:
		zw := zip.NewWriter(dw)
		closers = append(closers, zw)

		return &zipArchiveWriter{archiveClosers{closers}, zw, nil}, nil

	default:
		return nil, fmt.Errorf(
			"MIME type must be %s or %s but got %s",
			MIMEApplicationGZip, MIMEApplicationZip, mimeType)
	}
}

func (m *Master) getCheckpointStorageConfig(id uuid.UUID) (
	*expconf.CheckpointStorageConfig, error) {
	checkpoint, err := m.db.CheckpointByUUID(id)
	if err != nil || checkpoint == nil {
		return nil, err
	}

	bytes, err := json.Marshal(checkpoint.CheckpointTrainingMetadata.ExperimentConfig)
	if err != nil {
		return nil, err
	}

	legacyConfig, err := expconf.ParseLegacyConfigJSON(bytes)
	if err != nil {
		return nil, err
	}

	return ptrs.Ptr(legacyConfig.CheckpointStorage()), nil
}

func (m *Master) getCheckpoint(c echo.Context, mimeType string) error {
	args := struct {
		CheckpointUUID string `path:"checkpoint_uuid"`
	}{}
	if err := api.BindArgs(&args, c); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			"invalid checkpoint_uuid: "+err.Error())
	}

	checkpointUUID, err := uuid.Parse(args.CheckpointUUID)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest,
			fmt.Sprintf("unable to parse checkpoint UUID %s: %s",
				args.CheckpointUUID, err))
	}

	// Assume a checkpoint always has experiment configs
	storageConfig, err := m.getCheckpointStorageConfig(checkpointUUID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError,
			fmt.Sprintf("unable to retrieve experiment config for checkpoint %s: %s",
				args.CheckpointUUID, err.Error()))
	}
	if storageConfig == nil {
		return echo.NewHTTPError(http.StatusNotFound,
			fmt.Sprintf("checkpoint %s does not exist", args.CheckpointUUID))
	}

	c.Response().Header().Set(echo.HeaderContentType, mimeType)
	writerPipe, err := buildWriterPipeline(c.Response(), mimeType)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	downloader, err := newDownloader(storageConfig, writerPipe, args.CheckpointUUID)
	if err != nil {
		return err
	}

	err = downloader.download(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError,
			fmt.Sprintf("unable to download checkpoint %s: %s", args.CheckpointUUID, err.Error()))
	}

	err = writerPipe.Close()
	if err != nil {
		return err
	}
	c.Response().Flush()

	return nil
}

// @Summary Get a tarball of checkpoint contents.
// @Tags Checkpoints
// @ID get-checkpoint-tgz
// @Accept  json
// @Produce  application/gzip; charset=utf-8
// @Param   checkpoint_uuid path string  true  "Checkpoint UUID"
// @Success 200 {} string ""
//nolint:godot
// @Router /checkpoints/{checkpoint_uuid}/tgz [get]
func (m *Master) getCheckpointTgz(c echo.Context) error {
	return m.getCheckpoint(c, MIMEApplicationGZip)
}

// @Summary Get a zip of checkpoint contents.
// @Tags Checkpoints
// @ID get-checkpoint-zip
// @Accept  json
// @Produce  application/zip; charset=utf-8
// @Param   checkpoint_uuid path string  true  "Checkpoint UUID"
// @Success 200 {} string ""
//nolint:godot
// @Router /checkpoints/{checkpoint_uuid}/zip [get]
func (m *Master) getCheckpointZip(c echo.Context) error {
	return m.getCheckpoint(c, MIMEApplicationZip)
}
