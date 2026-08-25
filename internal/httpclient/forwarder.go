package httpclient

import (
	"bytes"
	"io"
	"net/http"
	"time"
)

const maxResponseBytes = 64 << 10 // 64KB, keeps requests.request_response well under Mongo's 16MB doc limit

var Client = &http.Client{
	Timeout: 30 * time.Second,
}

// Forward POSTs body to endpoint and returns the status code and response
// body. A non-nil error means the request never completed (network
// failure/timeout/DNS error) — callers should treat that as a failed
// delivery with status code 0, not retry it themselves (retry is manual).
func Forward(endpoint string, body []byte, contentType string) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := Client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, respBody, nil
}
