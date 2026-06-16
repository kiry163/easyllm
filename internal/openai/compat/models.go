package compat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/kiry163/easyllm/internal/model"
)

func (c *Client) ListModels(ctx context.Context) (*model.ListModelsResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= c.retry.maxRetries()+1; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		if c.apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			lastErr = err
			if !c.shouldRetry(ctx, attempt, 0, err) {
				return nil, lastErr
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("request failed: status=%d body=%s", resp.StatusCode, string(preview))
			if !c.shouldRetry(ctx, attempt, resp.StatusCode, nil) {
				return nil, lastErr
			}
			continue
		}
		defer resp.Body.Close()
		out, err := parseModelsResponse(resp.Body)
		if err != nil {
			return nil, err
		}
		out.Provider = c.providerName
		for i := range out.Models {
			out.Models[i].Provider = c.providerName
		}
		return out, nil
	}
	return nil, lastErr
}

func parseModelsResponse(r io.Reader) (*model.ListModelsResponse, error) {
	var raw struct {
		Object string           `json:"object"`
		Data   []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, err
	}
	out := &model.ListModelsResponse{
		Models: make([]model.ModelInfo, 0, len(raw.Data)),
		Raw:    raw,
	}
	for _, item := range raw.Data {
		info := model.ModelInfo{Raw: cloneMap(item)}
		if id, ok := item["id"].(string); ok {
			info.ID = id
		}
		if object, ok := item["object"].(string); ok {
			info.Object = object
		}
		if created, ok := numberAsInt64(item["created"]); ok {
			info.Created = created
		}
		if ownedBy, ok := item["owned_by"].(string); ok {
			info.OwnedBy = ownedBy
		}
		out.Models = append(out.Models, info)
	}
	return out, nil
}

func numberAsInt64(value any) (int64, bool) {
	switch current := value.(type) {
	case float64:
		return int64(current), true
	case int64:
		return current, true
	case int:
		return int64(current), true
	default:
		return 0, false
	}
}
