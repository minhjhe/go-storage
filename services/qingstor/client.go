package qingstor

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/yunify/qingstor-sdk-go/v3/config"
	"github.com/yunify/qingstor-sdk-go/v3/service"

	"github.com/Xuanwo/storage/define"
	"github.com/Xuanwo/storage/pkg/segment"
)

// Client is the qingstor object sotrage client.
//
//go:generate go run ../../internal/cmd/meta_gen/main.go
type Client struct {
	config  *config.Config
	service *service.Service
	bucket  *service.Bucket

	segments map[string]*segment.Segment
}

// SetupBucket will setup bucket for client.
func (c *Client) SetupBucket(bucketName, zoneName string) (err error) {
	errorMessage := "setup qingstor bucket failed: %w"

	if zoneName != "" {
		bucket, err := c.service.Bucket(bucketName, zoneName)
		if err != nil {
			return fmt.Errorf(errorMessage, err)
		}
		c.bucket = bucket
		return nil
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	url := fmt.Sprintf("%s://%s.%s:%d", c.config.Protocol, bucketName, c.config.Host, c.config.Port)

	r, err := client.Head(url)
	if err != nil {
		return fmt.Errorf(errorMessage, err)
	}
	if r.StatusCode != http.StatusTemporaryRedirect {
		err = fmt.Errorf("head status is %d instead of %d", r.StatusCode, http.StatusTemporaryRedirect)
		return fmt.Errorf(errorMessage, err)
	}

	// Example URL: https://bucket.zone.qingstor.com
	zoneName = strings.Split(r.Header.Get("Location"), ".")[1]
	bucket, err := c.service.Bucket(bucketName, zoneName)
	if err != nil {
		return fmt.Errorf(errorMessage, err)
	}
	c.bucket = bucket
	return
}

// NewFromConfig will create a new client from config.
func NewFromConfig(cfg *config.Config) (*Client, error) {
	errorMessage := "create new qingstor client from config failed: %w"

	srv, err := service.Init(cfg)
	if err != nil {
		return nil, fmt.Errorf(errorMessage, err)
	}
	return &Client{
		service:  srv,
		segments: make(map[string]*segment.Segment),
	}, nil
}

// NewFromHomeConfigFile will create a new client from default home config file.
func NewFromHomeConfigFile() (*Client, error) {
	errorMessage := "create new qingstor client from home config file failed: %w"

	cfg, err := config.NewDefault()
	if err != nil {
		return nil, fmt.Errorf(errorMessage, err)
	}
	return NewFromConfig(cfg)
}

// Stat implements Storager.Stat
func (c *Client) Stat(path string, option ...define.Option) (i define.Informer, err error) {
	panic("implement me")
}

// Delete implements Storager.Delete
func (c *Client) Delete(path string, option ...define.Option) (err error) {
	panic("implement me")
}

// Copy implements Storager.Copy
func (c *Client) Copy(src, dst string, option ...define.Option) (err error) {
	panic("implement me")
}

// Move implements Storager.Move
func (c *Client) Move(src, dst string, option ...define.Option) (err error) {
	panic("implement me")
}

// ListDir implements Storager.ListDir
func (c *Client) ListDir(path string, option ...define.Option) (dir chan *define.Dir, file chan *define.File, stream chan *define.Stream, err error) {
	panic("implement me")
}

// ReadFile implements Storager.ReadFile
func (c *Client) ReadFile(path string, option ...define.Option) (r io.ReadCloser, err error) {
	errorMessage := "qingstor ReadFile failed: %w"

	_ = parseOptionReadFile(option...)
	input := &service.GetObjectInput{}

	output, err := c.bucket.GetObject(path, input)
	if err != nil {
		return nil, fmt.Errorf(errorMessage, err)
	}
	return output.Body, nil
}

// WriteFile implements Storager.WriteFile
func (c *Client) WriteFile(path string, size int64, r io.ReadCloser, option ...define.Option) (err error) {
	errorMessage := "qingstor WriteFile failed: %w"

	defer r.Close()

	opts := parseOptionWriteFile(option...)
	input := &service.PutObjectInput{
		ContentLength: &size,
		Body:          r,
	}
	if opts.HasMd5 {
		input.ContentMD5 = &opts.Md5
	}
	if opts.HasStorageClass {
		input.XQSStorageClass = &opts.StorageClass
	}

	_, err = c.bucket.PutObject(path, input)
	if err != nil {
		return fmt.Errorf(errorMessage, err)
	}
	return nil
}

// ReadStream implements Storager.ReadStream
func (c *Client) ReadStream(path string, option ...define.Option) (r io.ReadCloser, err error) {
	panic("not supported")
}

// WriteStream implements Storager.WriteStream
func (c *Client) WriteStream(path string, r io.ReadCloser, option ...define.Option) (err error) {
	panic("not supported")
}

// InitSegment implements Storager.InitSegment
func (c *Client) InitSegment(path string, size int64, option ...define.Option) (err error) {
	errorMessage := "qingstor InitSegment failed: %w"

	if _, ok := c.segments[path]; ok {
		return fmt.Errorf("Segment %s has been initiated", path)
	}

	_ = parseOptionInitSegment(option...)
	input := &service.InitiateMultipartUploadInput{}

	output, err := c.bucket.InitiateMultipartUpload(path, input)
	if err != nil {
		return fmt.Errorf(errorMessage, err)
	}

	c.segments[path] = &segment.Segment{
		TotalSize: size,
		ID:        *output.UploadID,
		Parts:     make([]*segment.Part, 0),
	}
	return
}

// ReadSegment implements Storager.ReadSegment
func (c *Client) ReadSegment(path string, offset, size int64, option ...define.Option) (r io.ReadCloser, err error) {
	panic("implement me")
}

// WriteSegment implements Storager.WriteSegment
func (c *Client) WriteSegment(path string, offset, size int64, r io.ReadCloser, option ...define.Option) (err error) {
	errorMessage := "qingstor WriteSegment failed: %w"

	s, ok := c.segments[path]
	if !ok {
		return fmt.Errorf(errorMessage, fmt.Errorf("segment %s is not initiated", path))
	}

	p := &segment.Part{
		Offset: offset,
		Size:   size,
	}

	partNumber, err := s.GetPartIndex(p)
	if err != nil {
		return fmt.Errorf(errorMessage, err)
	}

	_, err = c.bucket.UploadMultipart(path, &service.UploadMultipartInput{
		PartNumber:    &partNumber,
		UploadID:      &s.ID,
		ContentLength: &size,
		Body:          r,
	})
	if err != nil {
		return fmt.Errorf(errorMessage, err)
	}

	err = s.InsertPart(p)
	if err != nil {
		return fmt.Errorf(errorMessage, err)
	}
	return
}

// CompleteSegment implements Storager.CompleteSegment
func (c *Client) CompleteSegment(path string, option ...define.Option) (err error) {
	errorMessage := "qingstor CompleteSegment failed: %w"

	s, ok := c.segments[path]
	if !ok {
		return fmt.Errorf(errorMessage, fmt.Errorf("segment %s is not initiated", path))
	}

	err = s.ValidateParts()
	if err != nil {
		return
	}

	objectParts := make([]*service.ObjectPartType, len(s.Parts))
	for k, v := range s.Parts {
		partNumber := k
		objectParts[k] = &service.ObjectPartType{
			PartNumber: &partNumber,
			Size:       &v.Size,
		}
	}

	_, err = c.bucket.CompleteMultipartUpload(path, &service.CompleteMultipartUploadInput{
		UploadID:    &s.ID,
		ObjectParts: objectParts,
	})
	if err != nil {
		return fmt.Errorf(errorMessage, err)
	}

	delete(c.segments, path)
	return
}

// AbortSegment implements Storager.AbortSegment
func (c *Client) AbortSegment(path string, option ...define.Option) (err error) {
	errorMessage := "qingstor AbortSegment failed: %w"

	s, ok := c.segments[path]
	if !ok {
		return fmt.Errorf(errorMessage, fmt.Errorf("segment %s is not initiated", path))
	}

	_, err = c.bucket.AbortMultipartUpload(path, &service.AbortMultipartUploadInput{
		UploadID: &s.ID,
	})
	if err != nil {
		return fmt.Errorf(errorMessage, err)
	}

	delete(c.segments, path)
	return
}