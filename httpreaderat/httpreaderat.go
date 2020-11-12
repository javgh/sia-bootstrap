package httpreaderat

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

type HTTPReaderAt struct {
	ContentLength  int64
	url            string
	etag           string
	ongoingRequest bool
	offset         int64
	response       *http.Response
}

func New(url string) (*HTTPReaderAt, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	if resp.Header.Get("accept-ranges") == "" {
		return nil, errors.New("Server needs to support range requests")
	}

	etag := resp.Header.Get("etag")
	if etag == "" {
		return nil, errors.New("Server needs to provide an ETag for the consensus download")
	}

	hra := HTTPReaderAt{
		ContentLength: resp.ContentLength,
		url:           url,
		etag:          etag,
	}

	resp.Body.Close()

	return &hra, nil
}

func (hra *HTTPReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if !hra.ongoingRequest || hra.offset != off {
		// Don't already have an ongoing request or need a new
		// one because the offset does not match.
		if hra.ongoingRequest {
			hra.response.Body.Close()
			hra.ongoingRequest = false
		}

		req, err := newRangeRequest(hra.url, off)
		if err != nil {
			return 0, err
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, err
		}

		etag := resp.Header.Get("etag")
		if etag != hra.etag {
			resp.Body.Close()
			return 0, errors.New("ETag reported by server has changed between requests")
		}

		hra.response = resp
		hra.ongoingRequest = true
		hra.offset = off
	}

	n, err := io.ReadFull(hra.response.Body, p)
	if err != nil {
		return n, err
	}
	hra.offset += int64(n)

	return n, nil
}

func (hra *HTTPReaderAt) Close() error {
	if !hra.ongoingRequest {
		return nil
	}

	hra.ongoingRequest = false
	return hra.response.Body.Close()
}

func newRangeRequest(url string, off int64) (*http.Request, error) {
	bytes := fmt.Sprintf("bytes=%d-", off)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Range", bytes)

	return req, nil
}
