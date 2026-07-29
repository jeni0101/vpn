package managerclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jeni0101/vpn/server/internal/manager"
	"github.com/jeni0101/vpn/server/internal/model"
)

type Client struct {
	http *http.Client
}

func New(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{http: &http.Client{Transport: transport, Timeout: 20 * time.Second}}
}

func (c *Client) Health(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/v1/health", nil, nil)
}

func (c *Client) Devices(ctx context.Context) ([]model.Device, error) {
	var response struct {
		Devices []model.Device `json:"devices"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/devices", nil, &response)
	return response.Devices, err
}

func (c *Client) CreateInvite(ctx context.Context, request manager.CreateRequest) (manager.InviteResult, error) {
	var response manager.InviteResult
	err := c.do(ctx, http.MethodPost, "/v1/devices/invite", request, &response)
	return response, err
}

func (c *Client) CreateStandard(ctx context.Context, request manager.CreateRequest) (manager.StandardResult, error) {
	var response manager.StandardResult
	err := c.do(ctx, http.MethodPost, "/v1/devices/standard", request, &response)
	return response, err
}

func (c *Client) Claim(ctx context.Context, request manager.ClaimRequest) (manager.ClaimResult, error) {
	var response manager.ClaimResult
	err := c.do(ctx, http.MethodPost, "/v1/enrollments/claim", request, &response)
	return response, err
}

func (c *Client) Revoke(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/v1/devices/"+url.PathEscape(id)+"/revoke", struct{}{}, nil)
}

func (c *Client) Rotate(ctx context.Context, id string) (manager.InviteResult, error) {
	var response manager.InviteResult
	err := c.do(ctx, http.MethodPost, "/v1/devices/"+url.PathEscape(id)+"/rotate", struct{}{}, &response)
	return response, err
}

func (c *Client) Usage(
	ctx context.Context,
	deviceID, bucket string,
	from, to time.Time,
) ([]model.UsagePoint, error) {
	query := url.Values{}
	query.Set("device_id", deviceID)
	query.Set("bucket", bucket)
	query.Set("from", from.UTC().Format(time.RFC3339))
	query.Set("to", to.UTC().Format(time.RFC3339))
	var response struct {
		Points []model.UsagePoint `json:"points"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/usage?"+query.Encode(), nil, &response)
	return response.Points, err
}

func (c *Client) Audit(ctx context.Context, limit int) ([]model.AuditEvent, error) {
	var response struct {
		Events []model.AuditEvent `json:"events"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/audit?limit="+strconv.Itoa(limit), nil, &response)
	return response.Events, err
}

func (c *Client) AddAudit(ctx context.Context, event model.AuditEvent) error {
	return c.do(ctx, http.MethodPost, "/v1/audit", event, nil)
}

func (c *Client) Import(ctx context.Context, request manager.ImportRequest) error {
	return c.do(ctx, http.MethodPost, "/v1/import", request, nil)
}

func (c *Client) do(ctx context.Context, method, path string, request, response any) error {
	var body io.Reader
	if request != nil {
		data, err := json.Marshal(request)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, method, "http://manager"+path, body)
	if err != nil {
		return err
	}
	if body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	httpResponse, err := c.http.Do(httpRequest)
	if err != nil {
		return err
	}
	defer httpResponse.Body.Close()
	data, err := io.ReadAll(io.LimitReader(httpResponse.Body, 256*1024))
	if err != nil {
		return err
	}
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		var apiError struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &apiError) == nil && apiError.Error != "" {
			return fmt.Errorf("manager: %s", apiError.Error)
		}
		return fmt.Errorf("manager returned HTTP %d", httpResponse.StatusCode)
	}
	if response == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, response); err != nil {
		return err
	}
	return nil
}

var ErrUnavailable = errors.New("manager unavailable")
